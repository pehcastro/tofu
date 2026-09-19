package konst

const (
	ContextCeilingTokens = 250000
	BandIdentityTokens   = 8000
	BandFactsTokens      = 12000
	BandWorkingSetTokens = 80000
	BandRecentTokens     = 30000
	ContextTargetTokens  = BandIdentityTokens + BandFactsTokens + BandWorkingSetTokens + BandRecentTokens
)

const (
	JudgeRequestTokenCeiling = 64000
	JudgeStateTokenCeiling   = 32000
	JudgeTimeoutMillis       = 2500
	JudgeRetries             = 1
	JudgeBackoffMillis       = 250

	ChoiceCeiling          = 255
	JudgeScoreLevelCeiling = 10
	JudgeSumTolerance      = 0.02

	ThresholdDeadBand = 0.06

	TransportErrorDetailBytes = 512
	TransportRequestIDBytes   = 8

	WhyBarWidthChars = 20
)

const (
	TurnOpenRouterModel      = "anthropic/claude-opus-5"
	TurnAnthropicModel       = "claude-opus-5"
	TurnCodexModel           = "gpt-5.6-sol"
	TurnAttemptTimeoutMillis = 30000
	TurnRetries              = 1
	TurnBackoffMillis        = 250
	TurnMaxBackoffMillis     = 2000
	TurnResultBytesCap       = 8192
	TurnMaxSteps             = 40
	TurnMaxDecisions         = 40
	TurnMaxWallClockMillis   = 300000
)

