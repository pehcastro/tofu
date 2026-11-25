package konst

const Version = "0.5.24"

const GateAskArgsBytes = 4096

const (
	ApprovalAllowForSeconds     = 3600
	ApprovalAllowForMostSeconds = 86400
	ApprovalReasonRunes         = 280
)

const LeadBashSoftLimitMillis = 10000

const (
	WallsWindowDays = 14
	WallDaysKept    = 30
	WallLineBytes   = 512
)

const (
	TUIWallBucketMillis = 1
	TUIWallBuckets      = 250
)

const (
	WallTierSUnderMillis = 100
	WallTierAUnderMillis = 300
	WallTierBUnderMillis = 700
	WallTierCUnderMillis = 1000
	WallTierDUnderMillis = 1500
)

const BoardReportedSentencesMost = 2

const (
	DocsPageLinesMin = 60
	DocsPageLinesMax = 120
)

const RecipeFailuresAside = 2

const SessionGenerationReads = 4

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
	ContextCeilingTokens       = 450000
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

	CarryStandingBytes      = 8000
	CarryStandingOrderBytes = 600
	CarryStandingPhrases    = "always|never|from now on|keep|do not|don't|dont"
	CarryStandingJudged     = 4
	CarryStandingAsks       = 24

	ForkTailTokens      = 20000
	ForkTailRoomPercent = 50
	ForkRoomMostPercent = 20
	ForkStateBytes      = 12000
	ForkStateWords      = 1200

	ContextTrimPercentOfTarget = 80
	TrimWarmSuffixTokens       = 8000
	TrimColdAfterIdleMinutes   = 90
	ArtifactHandleBytesAtMost  = 64

	ImageTokens         = 1600
	ImageResultsKept    = 20
	ImageResultsCeiling = 25

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

	CalibrateCutCeiling   = 3.0
	CalibrateCutSteps     = 24
	CalibrateGapTolerance = 0.02
	CalibrateSignificance = 0.05
	CalibrateErrorBins    = 10

	TransportErrorDetailBytes = 512
	TransportRequestIDBytes   = 8

	StreamReadBytes  = 64 << 10
	StreamLineBytes  = 16 << 20
	StreamIdleMillis = 60000

	CredLeaseTTLMillis       = 45000
	CredRefreshTimeoutMillis = 40000
	CredSweepSeconds         = 60
	CredRefreshAheadMinutes  = 10
	CredRefreshAfterDays     = 8
	CredWriteTries           = 3
	CredWriteRetryMillis     = 1000
	CredReloadRetries        = 1
	CredReloginWarnDays      = 7

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

	SettledSubAgentFoldMinutes = 120
	WorkingSubAgentNamesShown  = 7

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
	LongLivedGCPercent        = 200
	LongLivedMemoryLimitBytes = 512 * 1024 * 1024
	RecalledNewestGenerations = 2
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
	SubscriptionCacheHistory5m      = "history-5m"
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
	BackgroundReadyMillis = 2000
	GateAskWaitSeconds    = 180
	BashSoftLimitMillis   = 30000
	ShellWaitMillis       = 300000
	ShellWaitMatchMillis  = 1000
	ShellQuietIdleMillis  = 30000
	ShellIdleCPUMillis    = 50
	ShellWaitMissLines    = 5
	ShellNamedLogBytes    = 65536
	ShellTailBytes        = 262144

	ShellLogClockSlackMillis = 1000
	ShellRewriteHeadBytes    = 64
	ShellConsoleColumns      = 1024
	ShellConsoleRows         = 50
	ShellMinColumns          = 2
	ShellMaxRows             = 500
	ShellRawRingBytes        = 1048576
	ShellInputMaxBytes       = 65536
	ShellHandBackNoteChars   = 500
	ShellResizeAckMillis     = 150
	ShellResizeTries         = 3

	ShellLogBytes         = 1048576
	ShellLogHeadBytes     = 262144
	ShellLogTailBytes     = 262144
	ShellLogLineSeekBytes = 4096
	KeptShellsRunningMax  = 64

	TypecheckDeadlineMillis    = 15000
	TypecheckFirstCheckMillis  = 240000
	TypecheckInlineMillis      = 800
	TypecheckInlineFirstMillis = 3000
	TypecheckWatchNoticeMillis = 2000
	TypecheckLinesCap          = 20
	TypecheckWarmPackages      = 2

	TestRunDeadlineMillis = 180000
	TestRunnerWorkers     = 2
	TestLinesCap          = 20
	TestErrorLines        = 3
	TestReplyBytesCap     = 16777216
	OldestVitestMajor     = 2
	OldestVitestMinor     = 1

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
	SubAgentProseBytes       = 4000
	SubAgentReportBytes      = 7000
	SubAgentReportHeadBytes  = 300
	SubAgentReportFullBytes  = 300
	SubAgentAttemptsBytes    = 400
	SubAgentAttemptBytes     = 160
	SubAgentWroteBytes       = 400
	SubAgentImpactBytes      = 500
	SubAgentFindingsBytes    = 1600
	SubAgentFindingBytes     = 600
	SubAgentChecksBytes      = 1200
	SubAgentCheckBytes       = 240
	SubAgentLearnedBytes     = 300
	SubAgentLearnedItemBytes = 200
	SubAgentAskedBytes       = 300
	SubAgentBoardBytes       = 500
	SubAgentRunsAsBytes      = 200
	SubAgentRunErrorBytes    = 300
	SubAgentAskQuestionBytes = 300
	SubAgentAskWhyBytes      = 1200
	SubAgentAskDefaultBytes  = 300
	SubAgentAskOptionBytes   = 120
	SubAgentAskOptionsFewest = 2
	SubAgentAskOptionsMost   = 4
	SubAgentAskRefBytes      = 200
	SubAgentAskRefsMost      = 5
	SubAgentAskBriefBytes    = 300
	SubAgentAskAnswerBytes   = 1000
	SubAgentWarmMillis       = 8000
	SubAgentSleepSeconds     = 5
	SubAgentAskMillis        = 30000
	SubAgentAskLineRunes     = 200
	SubAgentAskRunningRunes  = 2000
	SubAgentGateAnswerMillis = 300000
	ReportGatherQuietMillis  = 1500
	ReportGatherCapMillis    = 10000
	SubAgentMaxForks         = 10
	SubAgentMaxSteps         = 150

	SubAgentWallClockSeconds = 3600

	OrchestratorSourceLinesPerTurn = 200
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
	MutateMutantsCap              = 50
	MutateChildMemoryCeilingBytes = 4 * 1024 * 1024 * 1024
	DiffTableMaxCells             = 10 * 1000 * 1000
)

const (
	MapBudgetTokens   = 1000
	MapRankDamping    = 0.85
	MapRankIterations = 50
	MapMentionWeight  = 10
	MapSignatureBytes = 120
)

const (
	APISignatureBytes = 2000
	APIDocBytes       = 1500
	APINames          = 80
	APIMatches        = 4
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

const TemporaryProjectIdleHours = 2

const (
	ProjectRegistryTries          = 20
	ProjectRegistryRetryMillis    = 50
	ProjectRegistryLockWaitMillis = 3000
)

const NearestWordBytesPerEdit = 3

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
	BrowserWakeMillis               = 3000
	BrowserSleepMinutesDefault      = 10

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
	CronPromptMark         = "), not typed by the person:\n"
	CronBookFile           = "cron.json"
)

const (
	SettleWaitMillis  = 500
	SettleRetryMillis = 2
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
	MemoryRuleWordsMost         = 25
	MemoryIdentityTimeoutMillis = 3000
	MemoryGhIDKeptHours         = 24
	MemorySaltBytes             = 16
	MemoryAuthorBytes           = 16
	MemoryAuthorScryptCost      = 1 << 15
	MemoryAuthorScryptBlock     = 8
	MemoryAuthorScryptThreads   = 1
	MemorySharedStemsToAskKept  = 2
)

const (
	MemtreeLineBytes         = 512
	MemtreeCompactionsAtOnce = 8
	MemtreeViewBytes         = 32 << 10
	MemtreeContextBytes      = 32 << 10
	MemtreeLockWaitMillis    = 5000
	MemtreeLockRetryMillis   = 10

	EpisodeViewPercentOfRoom = 50
)

const (
	UndoLockWaitMillis        = 60000
	UndoLockRetryMillis       = 25
	UndoLockLifetimeSeconds   = 600
	UndoSnapshotTimeoutMillis = 20000
)

const (
	HostEventBuffer      = 256
	HostSteeringQueue    = 64
	HostCloseWaitMillis  = 3000
	HostLingerMillis     = 5000
	HostReadyMillis      = 10000
	HostAnswerMillis     = 1000
	HostJoinMillis       = 120000
	HostRetryMillis      = 50
	HostAliveMillis      = 5000
	HostStalledMillis    = 300000
	FollowQueue          = 1024
	FollowQueueBytes     = 8 << 20
	FollowSettledKept    = 32
	FollowReplyMillis    = 3000
	FollowHubStartMillis = 20000
)

const (
	ServeQueue            = 128
	ServeKeptQueue        = 1024
	ServeLineBytes        = 16 << 20
	ServeShellPollMillis  = 250
	ServeStopMillis       = 10000
	ServeQuotaPollMinutes = 5
	ServeLedgerPageRows   = 200
	CPUsPerReadWorker     = 2
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
	LimitGraceMinutes          = 3
	LimitRetryMinutes          = 5
	LimitGiveUpMinutes         = 60
	LimitSecondsUnderMinutes   = 10
)

const (
	ScratchCleanupDays       = 7
	ScratchSweepHours        = 1
	SessionStaleDays         = 90
	ScratchMaxGB             = 50
	ScratchRemoveTries       = 5
	ScratchRemoveRetryMillis = 200
	ScratchReadBytes         = 64 << 10
)

const (
	AskPersonWaitSeconds          = 300
	AskPersonQuestionRunesMost    = 200
	AskPersonDescriptionRunesMost = 200
	AskPersonContextRunesMost     = 1500
	AskPersonQuestionsMost        = 4
	AskPersonOptionsLeast         = 2
	AskPersonOptionsMost          = 4
	AskPersonHeaderWordsMost      = 4
	UpgradeNoteBytes              = 6 << 10
	AskPersonLabelWordsMost       = 5
	BoardTLDRRunesMost            = 120
	BoardELI5LinesMost            = 3
	BoardReadersMost              = 8
	BoardHookDepthMost            = 3
	BoardCallerFilesMost          = 20000
	BoardCallerLineRunesMost      = 100
	LeadCheckInReviewsMost        = 10

	BoardAuditFreshDays        = 1
	BoardAuditStaleDays        = 7
	BoardAuditSlowPercentile   = 90
	BoardAuditDuplicateOverlap = 0.8

	BoardSaidSentencesMost = 3
	BoardSaidWordsMost     = 80

	BoardBypassWhyRunesMost = 200
	BoardBypassWaitHours    = 24
	BoardBypassFlagCount    = 3
	BoardBypassFlagDays     = 7

	BraveSearchDollarsPerCall = 5.0 / 1000
	KeyPollTimeoutMillis      = 5000
)

const (
	EnvProbeTimeoutMillis = 5000
	EnvManifestBytes      = 1 << 20
	EnvKeysShown          = 100
)

const (
	LspIdleMillis          = 300_000
	LspStartMillis         = 60_000
	LspRequestMillis       = 30_000
	LspShutdownMillis      = 3_000
	LspOpenedFiles         = 300
	SymbolsReferencesShown = 30
	SymbolsPreviewChars    = 120
	LspStderrLines         = 5
	LspStderrBytes         = 4096

	SymbolsDiagnosticsWaitMillis = 10_000
	LspDiagnosticsSettleMillis   = 700
)

const (
	ShadowSnapshotTimeoutMillis = 20000
	ShadowTurnsKept             = 1000
	ShadowStoreKeptBytes        = 536870912
	ChangesTurnsShown           = 20
)

const (
	CodemodRunMillis           = 60 * 1000
	CodemodFilesScanned        = 5000
	CodemodWorkersMax          = 12
	CodemodFileBytesSkipped    = 3 * 1000 * 1000
	CodemodFileLinesSkipped    = 200 * 1000
	CodemodSitesShown          = 40
	CodemodDiffBytes           = 16 * 1024
	CodemodCheckerBytes        = 4 * 1024
	CodemodIndentLookbackBytes = 512
)

const (
	ContextResultsShown     = 5
	ContextStaleShown       = 20
	ContextLabelChars       = 80
	ContextForgetOfferShare = 0.7
	BudgetSubAgentsShown    = 20
	NotesMost               = 20
	NoteBytes               = 500
)

const (
	ClaimsPartsChecked = 20
	ClaimsCallsOffered = 40
	ClaimsArgsBytes    = 200
	ClaimsOutputBytes  = 600
)

const (
	EstimateSampleRuns     = 10
	EstimateDoneByFactor   = 1.5
	EstimateFirstRunFactor = 1.75
	EstimateShortMillis    = 120_000
	EstimateShortPadMillis = 60_000

	EstimateCommandFieldsRead = 3
)

const (
	TokenRateMinTokens      = 200
	TokenRateMinMillis      = 4_000
	TokenRateHalfLifeMillis = 20_000
	TokenRateBucketMillis   = 250
	TokenRateCharsPerToken  = 4
	TokenRatePushMillis     = 1_000
)

const TestCPUs = 4

const FormatRunMillis = 60_000

const (
	ProfileRunMillis   = 300_000
	ProfileRows        = 10
	ProfileOutputLines = 20
)

const (
	RecordRunMillis   = 120_000
	RecordBodyBytes   = 8 << 20
	RecordOutputLines = 40
)

const (
	DBRowsShown   = 20
	DBCellBytes   = 120
	DBQueryMillis = 10_000
)

const (
	HTTPReadBytes  = 16 << 20
	HTTPShownBytes = 2048
	HTTPItemsShown = 3
	HTTPCallMillis = 30_000
)

const (
	DepsRequestMillis   = 10_000
	DepsRunMillis       = 120_000
	DepsAdvisoriesShown = 10
	DepsOutputLines     = 40
)

const (
	ReadWindowLines    = 100
	ReadClosestSymbols = 5
)

const BashDiskWalkEntries = 20000

const (
	WhatifCheckMillis   = 180_000
	OverlayStaleMinutes = 10
	RaceBriefsFewest    = 2
	RaceBriefsMost      = 3
	RaceKeptMinutes     = 60
)

const (
	EvalRunMillis   = 180_000
	EvalStdoutLines = 20
	EvalValueBytes  = 4000
)

const (
	ShapeSampleBytes       = 16 << 20
	ShapeErrorContextBytes = 40
)

const (
	TypecheckExplainWindowLines  = 5
	TypecheckExplainErrors       = 10
	TypecheckExplainPlaces       = 5
	TypecheckExplainDeclarations = 3
)

const (
	DebugRequestMillis       = 10_000
	DebugRunMillis           = 120_000
	DebugStopMillis          = 3_000
	DebugStackFrames         = 5
	DebugLocals              = 30
	DebugValueRunes          = 120
	DebugOutputLines         = 8
	DebugStatementProbeLines = 20
	DebugIdleMillis          = 300_000

	DebugTraceCalls        = 20
	DebugTraceInstructions = 4096
)

const (
	ShellDrainLinesFloor        = 50
	ShellDrainSimilarityPercent = 40
	ShellDrainTemplatesCap      = 30
)
