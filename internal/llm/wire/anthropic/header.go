package anthropic

import (
	"runtime"
	"slices"
	"strings"
)

type Header struct {
	Name  string
	Value string
}

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
			{"Accept", accept},
			{"Accept-Encoding", "gzip, deflate, br, zstd"},
			{"Connection", "keep-alive"},
			{"Content-Type", "application/json"},
			{"anthropic-version", AnthropicAPIVersion},
			{"anthropic-dangerous-direct-browser-access", "true"},
			{"x-app", "cli"},
		}
		if beta := BetaHeader(nil, options.ExtraBetas); beta != "" {
			headers = append(headers, Header{"anthropic-beta", beta})
		}
		return append(headers, Header{"X-Api-Key", options.Token})
	}

	base := ClaudeCodeUtilityBetas()
	if options.AgentRequest {
		base = ClaudeCodeAgentBetas(options.Thinking)
	}

	headers := []Header{
		{"Accept", "application/json"},
		{"Content-Type", "application/json"},
		{"User-Agent", ClaudeCodeUserAgent},
	}
	if options.SessionID != "" {
		headers = append(headers, Header{"X-Claude-Code-Session-Id", options.SessionID})
	}
	headers = append(headers,
		Header{"X-Stainless-Arch", stainlessArch(runtime.GOARCH)},
		Header{"X-Stainless-Lang", "js"},
		Header{"X-Stainless-OS", stainlessOS(runtime.GOOS)},
		Header{"X-Stainless-Package-Version", PinnedAnthropicSDKVersion},
		Header{"X-Stainless-Retry-Count", "0"},
		Header{"X-Stainless-Runtime", "node"},
		Header{"X-Stainless-Runtime-Version", PinnedNodeRuntimeVersion},
		Header{"X-Stainless-Timeout", PinnedStainlessTimeoutSeconds},
	)
	if beta := BetaHeader(base, options.ExtraBetas); beta != "" {
		headers = append(headers, Header{"anthropic-beta", beta})
	}
	return append(headers,
		Header{"anthropic-dangerous-direct-browser-access", "true"},
		Header{"anthropic-version", AnthropicAPIVersion},
		Header{"Authorization", "Bearer " + options.Token},
		Header{"x-app", "cli"},
		Header{"Connection", "keep-alive"},
		Header{"Accept-Encoding", "gzip, deflate, br, zstd"},
	)
}
