package codexsub

import (
	"cmp"
	"os"

	"tofu/internal/models/llm/accounts"
)

const (
	TokenURLVariable = "TOFU_CODEX_TOKEN_URL"
	clientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	authClaim        = "https://api.openai.com/auth"
)

func Spec() accounts.Spec {
	return accounts.Spec{
		Provider:     accounts.CodexSub,
		ClientID:     clientID,
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
		TokenURL:           cmp.Or(os.Getenv(TokenURLVariable), "https://auth.openai.com/oauth/token"),
		TokenBody:          accounts.BodyForm,
		IdentityTokenField: "id_token",
		AccountIDPath:      authClaim + ".chatgpt_account_id",
		EmailPath:          "email",
		Instructions:       "A browser window should open. Complete login to finish.",
	}
}
