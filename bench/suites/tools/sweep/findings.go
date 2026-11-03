package sweep

type Reader struct {
	Package string
	Dir     string
	Differs string
}

func SessionReaders() []Reader {
	return []Reader{
		{Package: "bench/corpus", Dir: "~/.tofu/projects/<folder>-<hash8>/sessions",
			Differs: "the shared one. ReadTurn, ReadTurnDir and WalkSessions read every turn and every sub-agent run through internal/store/session.Store, whether it sits in a session folder or in the single-file or header-plus-body.jsonl layout before it, into RecordedTurn. 19 other bench packages import it."},
		{Package: "bench/suites/context/schemas", Dir: ".tofu/sessions",
			Differs: "reimplements the same header.json plus body.jsonl walk from scratch, in ReadSessions, to reach StepUsage.PromptTokens/CacheReadTokens/CacheWriteTokens/CompletionTokens, fields RecordedStep does not carry."},
		{Package: "bench/suites/agent/turn", Dir: ".tofu/sessions",
			Differs: "ReadCorpus re-walks the directory itself, re-doing the file-vs-jsonl-dir classification WalkSessions already does, so it can additionally record JSONLDirs/JSONLDirsSeen/JSONLDirsSkipped. It calls corpus.ReadTurn and corpus.ReadTurnDir per entry rather than corpus.WalkSessions."},
		{Package: "bench/suites/context/recall", Dir: ".tofu/sessions",
			Differs: "does not use bench/corpus at all. WalkCorpusReach reads through internal/store/session.Store, because it needs StepRow.Occupancy, StepRow.Fork and StepRow.Compaction, none of which RecordedStep carries."},
		{Package: "bench/suites/judge/picker", Dir: ".tofu/sessions and .tofu/log",
			Differs: "Gather does a raw byte scan of every .json/.jsonl file under the given roots for a line containing \"windows\", looking for quota readings. It does not parse a turn or a ledger row at all, and it is the one reader pointed at both directories at once."},
	}
}

func LogReaders() []Reader {
	return []Reader{
		{Package: "internal/judge/ledger", Dir: ".tofu/log",
			Differs: "the shared one. NewReader reads the day-stamped NNNN-NN-NN.jsonl ledger rows. bench/suites/judge/calibration, bench/kit/harness (parse_tofu.go) and bench/stopcheck (run.go) call it rather than reading the directory themselves."},
		{Package: "bench/suites/rules/rules", Dir: ".tofu/log",
			Differs: "ReadDir globs *.rules.jsonl, a different file suffix carrying rule-fire records (RuleID, Target, Mode, Blocked, Findings, At), not a ledger row. No overlap with ledger.NewReader's file glob, so this is a second stream in the same directory rather than the same one read twice."},
		{Package: "bench/suites/judge/picker", Dir: ".tofu/sessions and .tofu/log",
			Differs: "see above: the same raw byte scan for quota windows also walks .tofu/log."},
	}
}

type Duplication struct {
	A              string
	B              string
	Recommendation string
}

func Duplications() []Duplication {
	return []Duplication{
		{
			A:              "bench/suites/agent/turn/corpus.go: ReadCorpus (walks the dir, classifies file vs jsonl-dir, calls corpus.ReadTurn/ReadTurnDir)",
			B:              "bench/corpus/reader.go: WalkSessions (does the identical walk and classification)",
			Recommendation: "merge: have ReadCorpus call corpus.WalkSessions and layer JSONLDirs/JSONLDirsSeen/JSONLDirsSkipped on top of the result, instead of re-walking the directory by hand.",
		},
		{
			A:              "bench/suites/context/schemas/turns.go: ReadSessions, readSessionDir, readSessionFile (its own header.json/body.jsonl parse)",
			B:              "bench/corpus/reader.go: ReadTurn, ReadTurnDir (the same parse, already scrubbed and schema-tagged)",
			Recommendation: "leave, with a reason: corpus.RecordedStep carries no token-usage fields, so schemas cannot get what it needs from corpus today. The one-line fix is adding PromptTokens/CacheReadTokens/CacheWriteTokens/CompletionTokens to corpus.RecordedStep, at which point schemas should read through corpus like everyone else. That is a go-dev ticket, not this one.",
		},
		{
			A:              "bench/corpus/reader.go: type SkippedTurn{Path, Reason}",
			B:              "bench/suites/agent/turn/corpus.go: type SkippedTurn{Path, Reason} (same name, same shape, different package) and bench/suites/context/schemas/turns.go: type SkippedSession{Path, Reason}, bench/suites/context/recall/reach.go: type SkippedSession{ID, Reason}, bench/stopcheck/session.go: type Skipped{File, Why}, bench/suites/judge/picker/reading.go: type Skip{Path, Field}",
			Recommendation: "merge: six hand-rolled two-field skip records for the same idea, a path or id and a reason a row did not count. One exported type in bench/corpus, reused everywhere a walker skips a row.",
		},
		{
			A:              "bench/kit/harness/repeats.go: SpreadOf (sorts, takes the middle, low, high by hand)",
			B:              "bench/stat/stat.go: Median, Percentile, Spread (the same three numbers, one call each)",
			Recommendation: "merge: SpreadOf should call stat.Median and stat.Spread instead of re-sorting and re-deriving the middle index.",
		},
		{
			A:              "bench/suites/context/thrift/spread.go: percentile (nearest-rank, int64, truncated index)",
			B:              "bench/stat/stat.go: Percentile (linear interpolation, float64)",
			Recommendation: "leave, with a reason: thrift measures byte and token counts, where a fractional value between two real counts is not a meaningful answer, so nearest-rank is the right rule and float64 interpolation is not a drop-in replacement.",
		},
	}
}

type Shape struct {
	Name     string
	Packages []string
	Against  string
}

func RepeatedShapes() []Shape {
	return []Shape{
		{Name: "percentile / median of a float64 series",
			Packages: []string{"bench/suites/judge/cost/report.go (medianLatency, calls stat.Median correctly)", "bench/kit/harness/repeats.go (SpreadOf, does not call bench/stat)", "bench/suites/tools/search/cost_test.go (median, a test-local helper on time.Duration)"},
			Against:  "bench/stat.Median and bench/stat.Percentile cover this. Only bench/suites/judge/cost uses it; harness and the search test each grew their own."},
		{Name: "percentile / spread of an int64 series (byte and token counts)",
			Packages: []string{"bench/suites/context/thrift/spread.go (Measure/percentile, nearest-rank)"},
			Against:  "bench/stat has no int64 form and no nearest-rank form. Not a bug: see the not-duplication note below."},
		{Name: "a two-field skip record with a path/id and a reason",
			Packages: []string{"bench/corpus (SkippedTurn)", "bench/suites/agent/turn (SkippedTurn)", "bench/suites/context/schemas (SkippedSession)", "bench/suites/context/recall (SkippedSession)", "bench/stopcheck (Skipped)", "bench/suites/judge/picker (Skip)"},
			Against:  "bench/stat has nothing for this; it is not a stat/stat.go shape at all, but it is the single most repeated shape found in this sweep."},
	}
}

type NotDuplication struct {
	What string
	Why  string
}

func NotDuplications() []NotDuplication {
	return []NotDuplication{
		{
			What: "every package that accumulates a running cost with a `+=` loop: bench/suites/judge/cost (arm.Total.Money), bench/kit/api (result.TotalCost from call.Response.Usage.Cost), bench/suites/judge/wording (V1TotalCost, V2TotalCost), bench/suites/agent/turn (result.TotalCostUSD)",
			Why:  "each sums a different field off a different struct for a different question: jev decision cost, a recorded transcript's own dollar total, an A/B wording comparison, a turn's wall-clock cost. A shared `Sum(values []float64) float64` would save one line per call site and cost a level of indirection between the reader and the field actually being summed. Not worth it; this is what \"two packages measuring different things with similar code\" looks like.",
		},
	}
}
