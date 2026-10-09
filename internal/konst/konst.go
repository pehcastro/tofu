package konst

const Version = "0.5.8"

const GateAskArgsBytes = 4096

const RecipeFailuresAside = 2

const RecipesPerTask = 2

const (
	RecipeShortestWord    = 3
	RecipeShortestEncoded = 16
	RecipeShortestDigitID = 9
	RecipeHoursPerDay     = 24
	RecipePlaceIDPrefix   = "ChIJ"
	RecipeCodeJoiners     = "_:"
	RecipeEncodedAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz+/=-_"
)

const (
	ContextCeilingTokens       = 250000
	ContextOutputReserveTokens = 20000
	ContextForkPercentOfUsable = 80

	BandShareWhole      = 10000
	BandIdentityShare   = 480
	BandFactsShare      = 120
	BandWorkingSetShare = 720
	BandRecentShare     = 1200

	MessageFramingTokens = 40

	CarrySignpostBytes = 120
	FactSignpostBytes  = 64
	FactSheetLines     = 64
	CarryActLines      = 10

	CarrySaidBytes        = 24000
	CarrySaidMessageBytes = 4000
	CarryLastWordBytes    = 16000

	ForkTailTokens      = 20000
	ForkTailRoomPercent = 50
	ForkStateBytes      = 12000
	ForkStateWords      = 1200

	ContextTrimPercentOfTarget = 80

	ImageTokens      = 1600
	ImageResultsKept = 20

	CarryNotLookups = "write edit message spawn plan remember rule_override ask settings lookup"

	CarryVolatileQueryKeys = "ref_fsid source_impression_id federated_search_session_id"
	CarryVolatileQueryPart = "session"
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

	CredLeaseTTLMillis       = 45000
	CredRefreshTimeoutMillis = 40000

	WhyStateBytes      = 2048
	MeterBarWidthChars = 12
)

const (
	ProgressTickMillis   = 80
	SubAgentRedrawMillis = 250
	QuitAgainMillis      = 3000
	StopSubAgentsMillis  = 3000
	FeedRecentEvents     = 200
	ComposerContentRows  = 10000

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
	ProseWidthChars = 80
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
	TurnResultLineWidth             = 768
	TurnOverflowKeepPercent         = 75
	IgnoreFileBytesCap              = 65536
	GlobPathsResultCap              = 300
	FetchLineWindow                 = 300
	ProjectInstructionsBytesDefault = 32768
	ProjectInstructionsBytesMost    = 262144
	DecisionCapMost                 = 1000
	TurnParallelToolCalls           = 4
	TurnMaxSteps                    = 40
	TurnLoopGuardRepeats            = 3
	TurnLoopGuardWindow             = 6
	TurnStepCapNoticeShare          = 0.1
	SubscriptionCacheTTL            = "1h"
	CacheBreakSlackTokens           = 50
	PromptHistoryEntries            = 1000
	PromptHistoryEntryBytes         = 16384
	PromptHistoryFileBytes          = 2097152
	PromptHistoryKeptBytes          = 1048576

	VerbMaxDepth      = 2
	VerbTimeoutMillis = 120000

	BashDeadlineMillis    = 120000
	BashMaxDeadlineMillis = 600000
	BashWaitDelayMillis   = 1000
	BashOutputHeldBytes   = 65536
	BackgroundYieldMillis = 10000
	BashSoftLimitMillis   = 30000
	ShellNamedLogBytes    = 65536
	ShellTailBytes        = 262144

	ShellLogClockSlackMillis = 1000
	ShellRewriteHeadBytes    = 64
	ShellConsoleColumns      = 1024
	ShellConsoleRows         = 50

	TypecheckDeadlineMillis    = 15000
	TypecheckFirstCheckMillis  = 240000
	TypecheckInlineMillis      = 400
	TypecheckInlineFirstMillis = 3000
	TypecheckWatchNoticeMillis = 2000
	TypecheckLinesCap          = 20
	TypecheckWarmPackages      = 2

	TestRunDeadlineMillis = 180000
	TestRunnerWorkers     = 2
	TestLinesCap          = 20
	TestErrorLines        = 3
	TestReplyBytesCap     = 16777216

	ShellProbeTimeoutMillis     = 2000
	ToolchainProbeTimeoutMillis = 2000
	PortCheckTimeoutMillis      = 300
	PortHolderTimeoutMillis     = 2000
	ReadyPollMillis             = 50

	ProjectSectionCap = 12

	SubAgentDepthDefault        = 2
	SubAgentsPerTurnDefault     = 10
	SubAgentCheckSecondsDefault = 1800
	SubAgentWatchSecondsDefault = 600

	SubAgentRetainedRows     = 4
	SubAgentMissionChars     = 60
	SubAgentMaxProcessDepth  = 1
	SubAgentCallsWatched     = 17
	SubAgentMaxRounds        = 3
	SubAgentReferenceBytes   = 16384
	SubAgentProseBytes       = 16384
	SubAgentProseEndBytes    = 2048
	SubAgentWarmMillis       = 8000
	SubAgentSleepSeconds     = 5
	SubAgentAskMillis        = 30000
	SubAgentGateAnswerMillis = 300000
	SubAgentMaxForks         = 10
	SubAgentMaxSteps         = 150

	SubAgentWallClockSeconds = 3600

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
	SearchLineWidth        = 512
	SearchFileByteCap      = 16 << 20
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

	SubAgentDiagnoseTailLines = 10
	FinishedShellKeptHours    = 72
)

const (
	CatalogStaleHours = 24
	CatalogRetryHours = 1
)

const (
	UpdateCheckHours   = 1
	UpdateWatchSeconds = 30
	UpdateProbeSeconds = 10
)

const (
	BrowserHostMessageBytes         = 1024 * 1024
	BrowserExtensionMessageBytes    = 64 * 1024 * 1024
	BrowserDialTimeoutMillis        = 2000
	BrowserCallTimeoutMillis        = 30000
	BrowserSocketPathMaxBytes       = 107
	BrowserSocketPathMaxBytesDarwin = 104
	BrowserIdleAfterMillis          = 1500

	BrowserPageTextRunes      = 6000
	BrowserElementCeiling     = 250
	BrowserActionCeiling      = 60
	BrowserStepsDefault       = 30
	BrowserDecisionsPerAction = 2
	BrowserRecentSteps        = 10
	BrowserNoChangeStop       = 3
	BrowserStaleStop          = 3
	BrowserSnapshotAttempts   = 10
	BrowserSnapshotGapMillis  = 20

	BrowserSnapshotMaxBytes     = 24576
	BrowserActTimeoutMillis     = 8000
	BrowserObserveTimeoutMillis = 20000
	BrowserSettlePollMillis     = 300
	BrowserWaitMaxMillis        = 10000
	BrowserScrollPixels         = 300
	BrowserCursorTextRunes      = 100
	BrowserURLQueryRunes        = 80
	BrowserDOMQuietMillis       = 300
	BrowserDOMQuietMaxMillis    = 1500
	BrowserTimerCountMaxMillis  = 1500
	BrowserCommitReturnMillis   = 1500
	BrowserChangesMaxBytes      = 1536
	BrowserChangeNodesMax       = 500
	BrowserBatchMax             = 10
	BrowserLoopWindow           = 20
	BrowserRepeatNotice         = 3
	BrowserRepeatRefuse         = 5
)

const (
	MotionRecordCeilingMillis   = 10000
	MotionWatchCeiling          = 10
	MotionBeforeMillisDefault   = 300
	MotionAfterMillisDefault    = 800
	MotionTakesDefault          = 3
	MotionViewportWidthDefault  = 960
	MotionViewportHeightDefault = 720
	MotionFrameLagMillis        = 30
	MotionGeometryThresholdPx   = 0.5
	MotionSheetMaxPx            = 2000
	MotionSheetMaxBytes         = 4718592
	MotionSheetGapPx            = 4
	MotionInspectColumns        = 4
	MotionInspectTilePx         = 400
	MotionInspectLabelScale     = 3
	MotionCompareColumns        = 8
	MotionCompareTilePx         = 240
	MotionCompareLabelScale     = 2
	MotionLabelInsetPx          = 6
	MotionLabelShadeAlpha       = 178
	MotionEmptyTileGrey         = 0xdd
	MotionCompareTakesAdvised   = 3
	MotionSettleMillisDefault   = 350
	MotionReadyTimeoutMillis    = 10000
	MotionDiscoverCeiling       = 250
	MotionNameChars             = 80
	MotionTransientJump         = 0.5
	MotionTransientReturn       = 0.25
	MotionTransientMaxMillis    = 300
)

const BrowserDeltaWholePercent = 70

const BrowserSettleTickMillis = 50

const (
	BrowserGuardWaitMillis  = 1500
	BrowserTargetNamesShown = 3
	BrowserGuardPollMillis  = 100
)

const (
	CronJobsMax            = 10
	CronFiresMax           = 50
	CronUnchangedStop      = 3
	CronMinIntervalSeconds = 60
	CronExpiryHours        = 24
	CronPollMillis         = 1000
	CronCheckTimeoutMillis = 60000
	CronCheckTailBytes     = 600
	CronSearchDays         = 366
)

const (
	RefusalCarriesWholeUpToBytes = 4096
	OmissionRefusedOverLostLines = 3
)

const (
	HookTimeoutSecondsDefault = 60
	HookTimeoutSecondsCeiling = 600
	HookProcessesAtOnce       = 4
	HookSlotPollMillis        = 50
	HookKillWaitMillis        = 2000
	HookOutputBytes           = 64 << 10
	HookContextBytes          = 10000
	HookSaidBytes             = 200
	HookStopContinuations     = 8
	HookSessionEndMillis      = 1500
)

const (
	LeadHandbackContinuations = 1
	LeadHandbackQuoteBytes    = 400
)

const (
	MemoryRuleBytes             = 160
	MemoryIdentityTimeoutMillis = 3000
	MemoryGhIDKeptHours         = 24
	MemorySaltBytes             = 16
	MemoryAuthorBytes           = 16
	MemoryAuthorScryptCost      = 1 << 15
	MemoryAuthorScryptBlock     = 8
	MemoryAuthorScryptThreads   = 1
)

const (
	MemtreeLineBytes         = 512
	MemtreeCompactionsAtOnce = 8
	MemtreeViewBytes         = 32 << 10
	MemtreeContextBytes      = 32 << 10

	EpisodeViewPercentOfRoom = 50
)

const (
	HostEventBuffer   = 256
	HostSteeringQueue = 64
)

const (
	ServeQueue            = 128
	ServeLineBytes        = 16 << 20
	ServeShellPollMillis  = 250
	ServeStopMillis       = 10000
	ServeQuotaPollMinutes = 5
)

const (
	QuotaPollTimeoutSeconds    = 15
	QuotaFreshMinutes          = 5
	QuotaJitter                = 0.25
	QuotaCoolFloorMinutes      = 1
	QuotaCoolCapMinutes        = 10
	QuotaHeardEverySeconds     = 60
	QuotaLoggedLookbackDays    = 7
	QuotaLockRetryMillis       = 25
	QuotaRenameTries           = 20
	QuotaNearLimitWatch        = 0.75
	QuotaNearLimitWatchSeconds = 120
	QuotaNearLimitClose        = 0.90
	QuotaNearLimitCloseSeconds = 60
	QuotaNearLimitAt           = 0.99
	QuotaNearLimitAtSeconds    = 30
)

const (
	AskPersonWaitSeconds    = 120
	AskPersonQuestionsMost  = 4
	AskPersonOptionsLeast   = 2
	AskPersonOptionsMost    = 4
	AskPersonHeaderRunes    = 12
	AskPersonLabelWordsMost = 5
)
