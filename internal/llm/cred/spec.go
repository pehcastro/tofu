package cred

import (
	"encoding/base64"
	"fmt"
	"time"
)

type Provider string

const (
	Anthropic Provider = "anthropic"
	Codex     Provider = "codex"
)

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
	Label               string
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
	switch Provider(name) {
	case Anthropic:
		return anthropicSpec(), nil
	case Codex:
		return codexSpec(), nil
	}
	return Spec{}, fmt.Errorf("cred: unknown provider %q, want anthropic or codex", name)
}

func anthropicSpec() Spec {
	clientID, _ := base64.StdEncoding.DecodeString(anthropicClientIDBase64)
	return Spec{
		Provider:     Anthropic,
		Label:        "Anthropic (Claude Pro/Max)",
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

func codexSpec() Spec {
	return Spec{
		Provider:     Codex,
		Label:        "ChatGPT Plus/Pro (Codex Subscription)",
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
