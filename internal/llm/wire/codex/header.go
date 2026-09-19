package codex

import (
	"strings"

	"boji/internal/sys"
)

type Header struct {
	Name  string
	Value string
}

type HeaderOptions struct {
	Token        string
	Subscription bool
	Claims       Claims
	Model        string
	ServiceTier  string
	Identity     Identity
	TurnMetadata string
	TurnState    string
}

func routingHint(model, serviceTier string) string {
	if serviceTier == "" {
		return "model=" + model
	}
	return "model=" + model + ";tier=" + serviceTier
}

func Headers(options HeaderOptions) []Header {
	headers := []Header{{HeaderAuthorization, "Bearer " + options.Token}}
	if !options.Subscription {
		return append(headers,
			Header{HeaderAccept, "text/event-stream"},
			Header{HeaderContentType, "application/json"})
	}

	if options.Claims.AccountID != "" {
		headers = append(headers, Header{HeaderAccountID, options.Claims.AccountID})
	}
	headers = append(headers, Header{HeaderRoutingHint, routingHint(options.Model, options.ServiceTier)})
	if options.Claims.Residency != "" {
		headers = append(headers, Header{HeaderResidency, options.Claims.Residency})
	}
	headers = append(headers,
		Header{HeaderBeta, BetaResponsesSSE},
		Header{HeaderOriginator, Originator},
		Header{HeaderVersion, PinnedCodexClientVersion},
		Header{HeaderUserAgent, UserAgentPrefix + sys.Version()},
	)
	if options.Identity.SessionID != "" {
		headers = append(headers,
			Header{HeaderConversationID, options.Identity.SessionID},
			Header{HeaderSessionID, options.Identity.SessionID},
			Header{HeaderClientRequestID, options.Identity.SessionID},
			Header{HeaderScopedSessionID, options.Identity.SessionID},
		)
	}
	headers = append(headers,
		Header{HeaderThreadID, options.Identity.ThreadID},
		Header{HeaderWindowID, options.Identity.WindowID},
		Header{HeaderTurnMetadata, options.TurnMetadata},
	)
	if options.TurnState != "" {
		headers = append(headers, Header{HeaderTurnState, options.TurnState})
	}
	return append(headers,
		Header{HeaderAccept, "text/event-stream"},
		Header{HeaderContentType, "application/json"})
}

func redactHeaderValue(name, value string) string {
	lower := strings.ToLower(name)
	if lower == "authorization" {
		return "Bearer " + RedactedCredential
	}
	if lower == HeaderAPIKey {
		return RedactedCredential
	}
	for _, marker := range []string{"account", "session", "conversation", "thread", "window", "installation"} {
		if strings.Contains(lower, marker) {
			return RedactedCredential
		}
	}
	if strings.HasPrefix(lower, "x-codex-turn") || lower == HeaderClientRequestID || lower == "cookie" {
		return RedactedCredential
	}
	return value
}
