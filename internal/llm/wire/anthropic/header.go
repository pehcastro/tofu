package anthropic

import (
	"runtime"
	"slices"
	"strings"

	"tofu/internal/llm"
)

type Header = llm.Header

type HeaderOptions struct {
	Token        string
	OAuth        bool
	Stream       bool
	AgentRequest bool
	Thinking     bool
	SessionID    string
	ExtraBetas   []string
}

func IsOfficialBaseURL(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(baseURL))
	return lower == OfficialBaseURL || strings.HasPrefix(lower, OfficialBaseURL+"/")
}

func IsOAuthToken(token string) bool {
	return strings.Contains(token, OAuthTokenMarker)
}

func stainlessArch(arch string) string {
	switch strings.ToLower(arch) {
	case "amd64", "x64":
		return "x64"
	case "arm64", "aarch64":
		return "arm64"
	case "386", "x86", "ia32":
		return "x86"
	}
	return "other::" + strings.ToLower(arch)
}

func stainlessOS(platform string) string {
	switch strings.ToLower(platform) {
	case "darwin":
		return "MacOS"
	case "windows", "win32":
		return "Windows"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	}
	return "Other::" + strings.ToLower(platform)
}

func BetaHeader(base, extra []string) string {
	seen := make(map[string]bool, len(base)+len(extra))
	kept := make([]string, 0, len(base)+len(extra))
	for _, beta := range slices.Concat(base, extra) {
		trimmed := strings.TrimSpace(beta)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, ",")
}

func Headers(options HeaderOptions) []Header {
	if !options.OAuth {
		accept := "application/json"
		if options.Stream {
			accept = "text/event-stream"
		}
		headers := []Header{
			{Name: "Accept", Value: accept},
			{Name: "Accept-Encoding", Value: "gzip, deflate"},
			{Name: "Connection", Value: "keep-alive"},
			{Name: "Content-Type", Value: "application/json"},
			{Name: "anthropic-version", Value: AnthropicAPIVersion},
			{Name: "anthropic-dangerous-direct-browser-access", Value: "true"},
			{Name: "x-app", Value: "cli"},
		}
		if beta := BetaHeader(nil, options.ExtraBetas); beta != "" {
			headers = append(headers, Header{Name: "anthropic-beta", Value: beta})
		}
		return append(headers, Header{Name: "X-Api-Key", Value: options.Token})
	}

	base := ClaudeCodeUtilityBetas()
	if options.AgentRequest {
		base = ClaudeCodeAgentBetas(options.Thinking)
	}

	headers := []Header{
		{Name: "Accept", Value: "application/json"},
		{Name: "Content-Type", Value: "application/json"},
		{Name: "User-Agent", Value: ClaudeCodeUserAgent},
	}
	if options.SessionID != "" {
		headers = append(headers, Header{Name: "X-Claude-Code-Session-Id", Value: options.SessionID})
	}
	headers = append(headers,
		Header{Name: "X-Stainless-Arch", Value: stainlessArch(runtime.GOARCH)},
		Header{Name: "X-Stainless-Lang", Value: "js"},
		Header{Name: "X-Stainless-OS", Value: stainlessOS(runtime.GOOS)},
		Header{Name: "X-Stainless-Package-Version", Value: PinnedAnthropicSDKVersion},
		Header{Name: "X-Stainless-Retry-Count", Value: "0"},
		Header{Name: "X-Stainless-Runtime", Value: "node"},
		Header{Name: "X-Stainless-Runtime-Version", Value: PinnedNodeRuntimeVersion},
		Header{Name: "X-Stainless-Timeout", Value: PinnedStainlessTimeoutSeconds},
	)
	if beta := BetaHeader(base, options.ExtraBetas); beta != "" {
		headers = append(headers, Header{Name: "anthropic-beta", Value: beta})
	}
	return append(headers,
		Header{Name: "anthropic-dangerous-direct-browser-access", Value: "true"},
		Header{Name: "anthropic-version", Value: AnthropicAPIVersion},
		Header{Name: "Authorization", Value: "Bearer " + options.Token},
		Header{Name: "x-app", Value: "cli"},
		Header{Name: "Connection", Value: "keep-alive"},
		Header{Name: "Accept-Encoding", Value: "gzip, deflate"},
	)
}
