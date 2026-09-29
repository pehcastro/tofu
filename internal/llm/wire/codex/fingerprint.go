package codex

import "tofu/internal/llm"

const (
	SubscriptionBaseURL = "https://chatgpt.com/backend-api"
	SubscriptionPath    = "/codex/responses"
	KeyBaseURL          = "https://api.openai.com/v1"
	KeyPath             = "/responses"
	MetaBaseURL         = "https://api.meta.ai/v1"
	ModelsPath          = "/models"

	CodexPackage             = "@openai/codex"
	PinnedCodexClientVersion = "0.153.0"
	Originator               = "tofu"
	UserAgentPrefix          = "tofu/"
	BetaResponsesSSE         = "responses=experimental"

	JWTAuthClaim          = "https://api.openai.com/auth"
	AccountClaim          = "chatgpt_account_id"
	PlanClaim             = "chatgpt_plan_type"
	DataResidencyClaim    = "chatgpt_data_residency"
	ComputeResidencyClaim = "chatgpt_compute_residency"

	HeaderAuthorization   = "Authorization"
	HeaderAPIKey          = "x-api-key"
	HeaderBeta            = "OpenAI-Beta"
	HeaderAccountID       = "chatgpt-account-id"
	HeaderOriginator      = "originator"
	HeaderVersion         = "version"
	HeaderUserAgent       = "User-Agent"
	HeaderConversationID  = "conversation_id"
	HeaderSessionID       = "session_id"
	HeaderClientRequestID = "x-client-request-id"
	HeaderScopedSessionID = "session-id"
	HeaderThreadID        = "thread-id"
	HeaderWindowID        = "x-codex-window-id"
	HeaderInstallationID  = "x-codex-installation-id"
	HeaderTurnMetadata    = "x-codex-turn-metadata"
	HeaderTurnState       = "x-codex-turn-state"
	HeaderRoutingHint     = "x-codex-routing-hint"
	HeaderResidency       = "x-openai-internal-codex-residency"
	HeaderAccept          = "accept"
	HeaderContentType     = "content-type"

	RequestKindTurn           = "turn"
	EncryptedReasoningInclude = "reasoning.encrypted_content"
	DefaultReasoningSummary   = "auto"
	TurnMetadataHeaderCap     = 100 << 10
	WhitespaceDeltaCap        = 256
	WhitespaceByteCap         = 16 << 10
)

func ReasoningEfforts() []llm.Effort {
	return []llm.Effort{
		llm.EffortNone, llm.EffortMinimal, llm.EffortLow,
		llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax,
	}
}

func ForbiddenSamplingControls() []string {
	return []string{
		"temperature", "top_p", "top_k", "min_p",
		"presence_penalty", "frequency_penalty", "repetition_penalty", "stop",
	}
}
