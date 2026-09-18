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
