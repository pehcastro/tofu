package anthropic

import (
	"slices"
	"strconv"
	"strings"

	"tofu/internal/llm"
)

//go:generate go run ../pins

const (
	ClaudeCodePackage             = "@anthropic-ai/claude-code"
	PinnedClaudeCodeVersion       = "2.1.284"
	PinnedAnthropicSDKVersion     = "0.112.1"
	PinnedNodeRuntimeVersion      = "v26.3.0"
	PinnedStainlessTimeoutSeconds = "600"

	VersionTooOldCode        = "claude_code_version_too_old"
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

func ClaudeCodeUserAgent(version string) string {
	return "claude-cli/" + version + " (external, cli)"
}

func NewerVersion(held, offered string) string {
	offeredParts, valid := versionParts(offered)
	if !valid {
		return held
	}
	heldParts, _ := versionParts(held)
	if slices.Compare(offeredParts, heldParts) > 0 {
		return offered
	}
	return held
}

func versionParts(version string) ([]int, bool) {
	fields := strings.Split(version, ".")
	if len(fields) != 3 {
		return nil, false
	}
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil || field[0] < '0' || field[0] > '9' {
			return nil, false
		}
		parts = append(parts, number)
	}
	return parts, true
}

func BillingFingerprintSourceIndexes() [3]int { return [3]int{4, 7, 20} }

func AnthropicBuiltinToolNames() [4]string {
	return [4]string{"web_search", "code_execution", "text_editor", "computer"}
}

func ReasoningEfforts() []llm.Effort {
	return []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax}
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
