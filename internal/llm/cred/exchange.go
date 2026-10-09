package cred

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tokenRequestTimeout = 30 * time.Second
	errorExcerptBytes   = 500
	verifierBytes       = 96
	stateBytes          = 32
)

type Identity struct {
	AccountID string `json:"account_id,omitempty"`
	Email     string `json:"email,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
	OrgName   string `json:"org_name,omitempty"`
}

type Credential struct {
	Provider       Provider  `json:"provider"`
	Kind           string    `json:"kind"`
	Access         string    `json:"access"`
	Refresh        string    `json:"refresh"`
	Expires        time.Time `json:"expires"`
	Identity       Identity  `json:"identity"`
	Authorized     time.Time `json:"authorized"`
	Refreshed      time.Time `json:"refreshed,omitzero"`
	RefreshExpires time.Time `json:"refresh_expires,omitzero"`
}

type tokenRefusal struct {
	status int
	code   string
	body   string
}

func (r tokenRefusal) Error() string {
	return fmt.Sprintf("token endpoint answered %d: %s", r.status, r.body)
}

func (r tokenRefusal) reason() string {
	switch {
	case r.code == "refresh_token_expired":
		return "the server says the refresh token expired"
	case r.code == "refresh_token_reused":
		return "the server says the refresh token was already used"
	case r.code == "refresh_token_invalidated":
		return "the server says the refresh token was revoked"
	case r.code == "invalid_grant" && r.status == http.StatusBadRequest:
		return "the server refused the refresh token (invalid_grant)"
	}
	return ""
}

func randomURLSafe(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func pkce() (verifier string, challenge string, err error) {
	verifier, err = randomURLSafe(verifierBytes)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func exchange(ctx context.Context, client *http.Client, spec Spec, code, state, redirectURI, verifier string, now time.Time) (Credential, error) {
	if hash := strings.Index(code, "#"); hash >= 0 {
		if echoed := code[hash+1:]; echoed != "" {
			state = echoed
		}
		code = code[:hash]
	}
	params := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     spec.ClientID,
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
	}
	if spec.SendStateOnExchange {
		params["state"] = state
	}
	body, err := postToken(ctx, client, spec, params, nil)
	if err != nil {
		return Credential{}, err
	}
	return mapCredential(spec, body, nil, now)
}

func refreshGrant(ctx context.Context, client *http.Client, spec Spec, stored Credential, now time.Time) (Credential, error) {
	if stored.Refresh == "" {
		return Credential{}, errors.New("cred: the stored credential has no refresh token, sign in again")
	}
	params := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     spec.ClientID,
		"refresh_token": stored.Refresh,
	}
	body, err := postToken(ctx, client, spec, params, spec.RefreshHeaders)
	if err != nil {
		return Credential{}, err
	}
	return mapCredential(spec, body, &stored, now)
}

func postToken(ctx context.Context, client *http.Client, spec Spec, params, headers map[string]string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenRequestTimeout)
	defer cancel()

	var payload io.Reader
	contentType := "application/x-www-form-urlencoded"
	if spec.TokenBody == BodyJSON {
		contentType = "application/json"
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(encoded)
	} else {
		form := url.Values{}
		for key, value := range params {
			form.Set(key, value)
		}
		payload = strings.NewReader(form.Encode())
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.TokenURL, payload)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Content-Type", contentType)

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cred: token request to %s failed: %w", spec.TokenURL, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		var detail map[string]any
		_ = json.Unmarshal(raw, &detail)
		code := cmp.Or(jsonPath(detail, "error.code"), jsonPath(detail, "error"), jsonPath(detail, "code"))
		return nil, tokenRefusal{status: response.StatusCode, code: strings.ToLower(code), body: string(raw[:min(len(raw), errorExcerptBytes)])}
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("cred: token response from %s was not JSON", spec.TokenURL)
	}
	return body, nil
}

func mapCredential(spec Spec, body map[string]any, previous *Credential, now time.Time) (Credential, error) {
	access := jsonPath(body, "access_token")
	if access == "" {
		return Credential{}, errors.New("cred: token response carried no access token")
	}
	seconds, ok := body["expires_in"].(float64)
	if !ok {
		return Credential{}, errors.New("cred: token response carried no expires_in")
	}
	credential := Credential{
		Provider:   spec.Provider,
		Kind:       KindOAuth,
		Access:     access,
		Refresh:    jsonPath(body, "refresh_token"),
		Expires:    now.Add(time.Duration(seconds)*time.Second - spec.ExpirySkew),
		Authorized: now,
		Refreshed:  now,
	}
	if seconds, given := body["refresh_token_expires_in"].(float64); given {
		credential.RefreshExpires = now.Add(time.Duration(seconds) * time.Second)
	}
	if previous != nil {
		if credential.Refresh == "" {
			credential.Refresh = previous.Refresh
			credential.RefreshExpires = cmp.Or(credential.RefreshExpires, previous.RefreshExpires)
		}
		credential.Identity = previous.Identity
		credential.Authorized = previous.Authorized
		return credential, nil
	}
	claims := body
	if spec.IdentityTokenField != "" {
		claims = jwtClaims(jsonPath(body, spec.IdentityTokenField))
	}
	credential.Identity = identityFrom(spec, claims)
	return credential, nil
}

func identityFrom(spec Spec, claims map[string]any) Identity {
	return Identity{
		AccountID: jsonPath(claims, spec.AccountIDPath),
		Email:     jsonPath(claims, spec.EmailPath),
		OrgID:     jsonPath(claims, spec.OrgIDPath),
		OrgName:   jsonPath(claims, spec.OrgNamePath),
	}
}

func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func jsonPath(body map[string]any, path string) string {
	var current any = body
	for path != "" {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		key, rest := longestKeyIn(object, path)
		current, path = object[key], rest
	}
	text, _ := current.(string)
	return text
}

func longestKeyIn(object map[string]any, path string) (string, string) {
	for cut := len(path); cut > 0; cut = strings.LastIndex(path[:cut], ".") {
		if _, held := object[path[:cut]]; held {
			return path[:cut], strings.TrimPrefix(path[cut:], ".")
		}
	}
	return path, ""
}
