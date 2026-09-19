package anthropic

const (
	PinnedClaudeCodeVersion       = "2.1.257"
	PinnedAnthropicSDKVersion     = "0.112.1"
	PinnedNodeRuntimeVersion      = "v26.3.0"
	PinnedStainlessTimeoutSeconds = "600"

	ClaudeCodeUserAgent      = "claude-cli/" + PinnedClaudeCodeVersion + " (external, cli)"
	ClaudeCodeSystemIdentity = "You are Claude Code, Anthropic's official CLI for Claude."
	ClaudeCodeToolPrefix     = "_"
	ClaudeCodeMaxTokens      = 64000

	AnthropicAPIVersion = "2023-06-01"
	OfficialBaseURL     = "https://api.anthropic.com"
	OAuthTokenMarker    = "sk-ant-oat"

	BillingHeaderPrefix        = "x-anthropic-billing-header:"
	BillingFingerprintSalt     = "59cf53e54c78"
	BillingCheckPlaceholder    = "cch=00000"
	BillingCheckSeed           = 0x4d659218e32a3268
	BillingCheckAnchorWindow   = 150
	BillingAnchor              = `"system":[{"type":"text","text":"` + BillingHeaderPrefix
	BillingFingerprintHexChars = 3
	BillingCheckHexChars       = 5

	DeviceIDInstallHashDomain = "omp-claude-device-id-v1:"
	DeviceIDAccountHashDomain = "omp-claude-device-id-v2"

	ExtendedCacheTTLBeta = "extended-cache-ttl-2025-04-11"
	NeverAdvertisedBeta  = "context-1m-2025-08-07"
)

func BillingFingerprintSourceIndexes() [3]int { return [3]int{4, 7, 20} }

func AnthropicBuiltinToolNames() [4]string {
	return [4]string{"web_search", "code_execution", "text_editor", "computer"}
}

func ClaudeCodeAgentBetas(thinking bool) []string {
	betas := []string{
		"claude-code-20250219",
		"oauth-2025-04-20",
		"interleaved-thinking-2025-05-14",
		"thinking-token-count-2026-05-13",
		"context-management-2025-06-27",
		"prompt-caching-scope-2026-01-05",
		"mid-conversation-system-2026-04-07",
	}
	if thinking {
		betas = append(betas, "effort-2025-11-24")
	}
	return append(betas, "fallback-credit-2026-06-01")
}

func ClaudeCodeUtilityBetas() []string {
	return []string{
		"oauth-2025-04-20",
		"interleaved-thinking-2025-05-14",
		"thinking-token-count-2026-05-13",
		"context-management-2025-06-27",
		"prompt-caching-scope-2026-01-05",
		"structured-outputs-2025-12-15",
	}
}
