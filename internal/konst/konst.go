package konst

const Version = "0.5.0-rc-fix7"

const (
	ContextCeilingTokens = 250000

	BandShareWhole      = 10000
	BandIdentityShare   = 480
	BandFactsShare      = 120
	BandWorkingSetShare = 720
	BandRecentShare     = 1200

	MessageFramingTokens = 40

	CarrySignpostBytes = 120
	FactSignpostBytes  = 64
	FactSheetLines     = 64
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

	StreamReadBytes  = 64 << 10
	StreamLineBytes  = 16 << 20
	StreamIdleMillis = 60000

	WhyBarWidthChars   = 20
	WhyStateBytes      = 2048
	MeterBarWidthChars = 12
)

const (
	ProgressTickMillis = 80
	QuitAgainMillis    = 3000
	FeedRecentEvents   = 200

	DriveTimeoutMillis = 60000
	DriveSettleMillis  = ProgressTickMillis / 2
	DrivePollMillis    = 5
	DriveMessageBuffer = 256
)

const (
	FrameBudgetMicros   = 16700
	FrameBudgetAttempts = 3
	FrameBudgetSamples  = 200
	FrameStallMicros    = 4000

	RewrapFillEntries = 4
	RewrapKeptWidths  = 4
	RewrapFillMillis  = 30
)

const (
	ReportWidthChars       = 62
	ReportLabelColumnChars = 12
	ProseWidthChars        = 80
)

const (
	TurnAttemptTimeoutMillis        = 30000
	TurnRetries                     = 4
	TurnBackoffMillis               = 250
	TurnMaxBackoffMillis            = 2000
	TurnBackoffGrowth               = 2
	TurnBackoffJitterFraction       = 0.2
	TurnTotalBackoffCeilingMillis   = 15000
	TurnResultBytesCap              = 32768
	IgnoreFileBytesCap              = 65536
	GlobPathsResultCap              = 300
	ProjectInstructionsBytesDefault = 32768
	ProjectInstructionsBytesMost    = 262144
	DecisionCapMost                 = 1000
	TurnParallelToolCalls           = 4
	TurnMaxSteps                    = 40
	TurnLoopGuardRepeats            = 3
	TurnLoopGuardWindow             = 6
	SubscriptionCacheTTL            = "1h"

	VerbMaxDepth      = 2
	VerbTimeoutMillis = 120000

	BashDeadlineMillis    = 120000
	BashMaxDeadlineMillis = 600000
	BashWaitDelayMillis   = 1000
	BackgroundYieldMillis = 10000

	TypecheckDeadlineMillis = 15000
	TypecheckLinesCap       = 20

	ShellProbeTimeoutMillis     = 2000
	ToolchainProbeTimeoutMillis = 2000
	PortCheckTimeoutMillis      = 300
	PortHolderTimeoutMillis     = 2000
	ReadyPollMillis             = 50

	ProjectSectionCap = 12

	SubAgentMaxDepth   = 2
	SubAgentMaxBreadth = 4

	SubAgentRetainedRows    = 4
	SubAgentMissionChars    = 60
	SubAgentMaxProcessDepth = 1
	SubAgentCallsWatched    = 17
	SubAgentMaxRounds       = 3
	SubAgentReferenceBytes  = 16384

	OrchestratorSourceLinesPerTurn = 10
)

const (
	SkillDescriptionCharacters     = 1024
	SkillListingUnknownCharacters  = 8000
	SkillListingWindowPercent      = 2
	SkillListingCharactersPerToken = 4
)

const (
	SearchTokenBudget      = 4000
	SearchBytesPerToken    = 4
	SearchResultOverhead   = 64
	SearchUnitHeaderTokens = 40
	SearchFrameLines       = 3
	SearchMatchLinesListed = 5
	SearchCandidateScanCap = 1200
)

const (
	SiftConcurrency        = 4
	SiftSignpostWordCap    = 8
	SiftReadWorthWordFloor = 15

	BrevityWordCap    = 120
	BrevityBoldCap    = 3
	BrevityMetricsCap = 2
)

const (
	MutateWorkers                 = 1
	MutateTimeoutCoefficient      = 5
	MutateChildMemoryCeilingBytes = 4 * 1024 * 1024 * 1024
	DiffTableMaxCells             = 10 * 1000 * 1000
)

const (
	DiffContextLinesTight   = 2
	DiffContextLinesDefault = 3
	DiffContextLinesWide    = 5
	DiffContextLinesWidest  = 8

	ShellLogTailLinesShort   = 100
	ShellLogTailLinesDefault = 500
	ShellLogTailLinesLong    = 1000
)
