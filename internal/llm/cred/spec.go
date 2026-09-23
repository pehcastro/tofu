package cred

import (
	"encoding/base64"
	"fmt"
	"time"
)

type Provider string

const (
	ClaudeSub Provider = "claude-sub"
	CodexSub  Provider = "codex-sub"
)

const (
	retiredClaudeWord = "anthropic"
	retiredCodexWord  = "codex"
)

func AllProviders() []Provider {
	return []Provider{ClaudeSub, CodexSub}
}

var retiredProviderWords = map[string]Provider{
	retiredClaudeWord: ClaudeSub,
	retiredCodexWord:  CodexSub,
}

func Canonical(name string) (Provider, bool) {
	for _, known := range AllProviders() {
		if name == string(known) {
			return known, true
		}
	}
	if source, retired := retiredProviderWords[name]; retired {
		return source, true
	}
	return "", false
}

func ParseProvider(name string) (Provider, error) {
	if source, ok := Canonical(name); ok {
		return source, nil
	}
	return "", fmt.Errorf("cred: unknown provider %q, want %s or %s", name, ClaudeSub, CodexSub)
}

type TokenBody string

const (
	BodyJSON TokenBody = "json"
	BodyForm TokenBody = "form"
)

const KindOAuth = "oauth"

const (
	anthropicClientIDBase64 = "OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl"
	codexClientID           = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthClaim          = "https://api.openai.com/auth"
	claudeCodeSDKVersion    = "0.112.1"
)

type Spec struct {
	Provider            Provider
	ClientID            string
	AuthorizeURL        string
	Scopes              []string
	AuthorizeParams     map[string]string
	CallbackPort        int
	CallbackPath        string
	PortFallback        bool
	TokenURL            string
	TokenBody           TokenBody
	SendStateOnExchange bool
	RefreshHeaders      map[string]string
	ExpirySkew          time.Duration
	GrantLife           time.Duration
	IdentityTokenField  string
	AccountIDPath       string
	EmailPath           string
	OrgIDPath           string
	OrgNamePath         string
	Instructions        string
}

func Lookup(name string) (Spec, error) {
	source, err := ParseProvider(name)
	if err != nil {
		return Spec{}, err
	}
	switch source {
	case ClaudeSub:
		return claudeSubSpec(), nil
	case CodexSub:
		return codexSubSpec(), nil
	}
	return Spec{}, err
}

func claudeSubSpec() Spec {
	clientID, _ := base64.StdEncoding.DecodeString(anthropicClientIDBase64)
	return Spec{
		Provider:     ClaudeSub,
		ClientID:     string(clientID),
		AuthorizeURL: "https://claude.ai/oauth/authorize",
		Scopes: []string{
			"org:create_api_key",
			"user:profile",
			"user:inference",
			"user:sessions:claude_code",
			"user:mcp_servers",
			"user:file_upload",
		},
		AuthorizeParams:     map[string]string{"code": "true"},
		CallbackPort:        54545,
		CallbackPath:        "/callback",
		PortFallback:        true,
		TokenURL:            "https://api.anthropic.com/v1/oauth/token",
		TokenBody:           BodyJSON,
		SendStateOnExchange: true,
		RefreshHeaders: map[string]string{
			"anthropic-beta": "oauth-2025-04-20",
			"User-Agent":     "anthropic-sdk-typescript/" + claudeCodeSDKVersion + " userOAuthProvider",
		},
		ExpirySkew:    5 * time.Minute,
		GrantLife:     30 * 24 * time.Hour,
		AccountIDPath: "account.uuid",
		EmailPath:     "account.email_address",
		OrgIDPath:     "organization.uuid",
		OrgNamePath:   "organization.name",
		Instructions: "Complete login in your browser. If the browser cannot reach this machine, " +
			"run again with --paste and paste the final redirect URL or authorization code.",
	}
}

func codexSubSpec() Spec {
	return Spec{
		Provider:     CodexSub,
		ClientID:     codexClientID,
		AuthorizeURL: "https://auth.openai.com/oauth/authorize",
		Scopes:       []string{"openid", "profile", "email", "offline_access", "api.connectors.read", "api.connectors.invoke"},
		AuthorizeParams: map[string]string{
			"id_token_add_organizations": "true",
			"codex_cli_simplified_flow":  "true",
			"originator":                 "tofu",
		},
		CallbackPort:       1455,
		CallbackPath:       "/auth/callback",
		PortFallback:       false,
		TokenURL:           "https://auth.openai.com/oauth/token",
		TokenBody:          BodyForm,
		IdentityTokenField: "id_token",
		AccountIDPath:      codexAuthClaim + ".chatgpt_account_id",
		EmailPath:          "email",
		Instructions:       "A browser window should open. Complete login to finish.",
	}
}
