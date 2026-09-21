package codex

import (
	"tofu/internal/llm"
	"tofu/internal/sys"
)

type Header = llm.Header

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
	headers := []Header{{Name: HeaderAuthorization, Value: "Bearer " + options.Token}}
	if !options.Subscription {
		return append(headers,
			Header{Name: HeaderAccept, Value: "text/event-stream"},
			Header{Name: HeaderContentType, Value: "application/json"})
	}

	if options.Claims.AccountID != "" {
		headers = append(headers, Header{Name: HeaderAccountID, Value: options.Claims.AccountID})
	}
	headers = append(headers, Header{Name: HeaderRoutingHint, Value: routingHint(options.Model, options.ServiceTier)})
	if options.Claims.Residency != "" {
		headers = append(headers, Header{Name: HeaderResidency, Value: options.Claims.Residency})
	}
	headers = append(headers,
		Header{Name: HeaderBeta, Value: BetaResponsesSSE},
		Header{Name: HeaderOriginator, Value: Originator},
		Header{Name: HeaderVersion, Value: PinnedCodexClientVersion},
		Header{Name: HeaderUserAgent, Value: UserAgentPrefix + sys.Version()},
	)
	if options.Identity.SessionID != "" {
		headers = append(headers,
			Header{Name: HeaderConversationID, Value: options.Identity.SessionID},
			Header{Name: HeaderSessionID, Value: options.Identity.SessionID},
			Header{Name: HeaderClientRequestID, Value: options.Identity.SessionID},
			Header{Name: HeaderScopedSessionID, Value: options.Identity.SessionID},
		)
	}
	headers = append(headers,
		Header{Name: HeaderThreadID, Value: options.Identity.ThreadID},
		Header{Name: HeaderWindowID, Value: options.Identity.WindowID},
		Header{Name: HeaderTurnMetadata, Value: options.TurnMetadata},
	)
	if options.TurnState != "" {
		headers = append(headers, Header{Name: HeaderTurnState, Value: options.TurnState})
	}
	return append(headers,
		Header{Name: HeaderAccept, Value: "text/event-stream"},
		Header{Name: HeaderContentType, Value: "application/json"})
}
