package llm

import (
	"slices"
	"strings"
)

const Redacted = "<redacted>"

type Header struct {
	Name  string
	Value string
}

type Dump struct {
	Method      string
	URL         string
	Headers     []Header
	Body        []byte
	Identifiers []string
}

func HeadersSafeInClear() []string {
	return []string{
		"accept",
		"accept-encoding",
		"connection",
		"content-type",
		"user-agent",
		"anthropic-beta",
		"anthropic-dangerous-direct-browser-access",
		"anthropic-version",
		"x-app",
		"x-stainless-arch",
		"x-stainless-lang",
		"x-stainless-os",
		"x-stainless-package-version",
		"x-stainless-retry-count",
		"x-stainless-runtime",
		"x-stainless-runtime-version",
		"x-stainless-timeout",
		"openai-beta",
		"originator",
		"version",
		"x-codex-routing-hint",
		"x-openai-internal-codex-residency",
	}
}

func RedactHeader(name, value string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "authorization" {
		return "Bearer " + Redacted
	}
	if slices.Contains(HeadersSafeInClear(), lower) {
		return value
	}
	return Redacted
}

func (d Dump) String() string {
	var out strings.Builder
	out.WriteString(d.Method + " " + d.URL + "\n")
	for _, header := range d.Headers {
		out.WriteString(header.Name + ": " + RedactHeader(header.Name, header.Value) + "\n")
	}
	body := string(d.Body)
	for _, identifier := range d.Identifiers {
		if identifier != "" {
			body = strings.ReplaceAll(body, identifier, Redacted)
		}
	}
	out.WriteString("\n" + body + "\n")
	return out.String()
}
