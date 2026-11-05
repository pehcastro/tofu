package claudesub

import (
	"cmp"
	"encoding/base64"
	"os"
	"time"

	"tofu/internal/models/llm/accounts"
)

const (
	TokenURLVariable     = "TOFU_CLAUDE_TOKEN_URL"
	clientIDBase64       = "OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl"
	claudeCodeSDKVersion = "0.112.1"
)

func Spec() accounts.Spec {
	clientID, _ := base64.StdEncoding.DecodeString(clientIDBase64)
	return accounts.Spec{
		Provider:     accounts.ClaudeSub,
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
		TokenURL:            cmp.Or(os.Getenv(TokenURLVariable), "https://api.anthropic.com/v1/oauth/token"),
		TokenBody:           accounts.BodyJSON,
		SendStateOnExchange: true,
		RefreshHeaders: map[string]string{
			"anthropic-beta": "oauth-2025-04-20",
			"User-Agent":     "anthropic-sdk-typescript/" + claudeCodeSDKVersion + " userOAuthProvider",
		},
		ExpirySkew:    5 * time.Minute,
		AccountIDPath: "account.uuid",
		EmailPath:     "account.email_address",
		OrgIDPath:     "organization.uuid",
		OrgNamePath:   "organization.name",
		Instructions: "Complete login in your browser. If the browser cannot reach this machine, " +
			"run again with --paste and paste the final redirect URL or authorization code.",
	}
}
