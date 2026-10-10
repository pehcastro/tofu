package wrongpath

import (
	"math"
	"sort"
	"time"

	"tofu/bench/corpus"
	"tofu/bench/stat"
)

const ratePerCalls = 100

var armSizes = []int{10, 30}

type Spread struct {
	Zeros  int
	Sum    int
	Mean   float64
	Median float64
	P90    float64
	Worst  int
	Range  int
	SD     float64
}

func measure(values []int) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	asFloat := make([]float64, len(values))
	spread := Spread{Worst: values[0]}
	low := values[0]
	for i, value := range values {
		asFloat[i] = float64(value)
		spread.Sum += value
		if value == 0 {
			spread.Zeros++
		}
		if value > spread.Worst {
			spread.Worst = value
		}
		if value < low {
			low = value
		}
	}
	spread.Range = spread.Worst - low
	spread.Mean = float64(spread.Sum) / float64(len(values))
	spread.Median = stat.Median(asFloat)
	spread.P90 = stat.Percentile(asFloat, 90)
	for _, value := range asFloat {
		spread.SD += (value - spread.Mean) * (value - spread.Mean)
	}
	spread.SD = math.Sqrt(spread.SD / float64(len(values)))
	return spread
}

type Detectable struct {
	ArmSessions    int
	Difference     float64
	FractionOfMean float64
}

type Shape struct {
	Name          string
	Rule          string
	FalsePositive string
	PerSession    Spread
	RatePerCalls  float64
	Detectable    []Detectable
}

func shapeOf(name, rule, falsePositive string, values []int, calls int) Shape {
	spread := measure(values)
	shape := Shape{
		Name:          name,
		Rule:          rule,
		FalsePositive: falsePositive,
		PerSession:    spread,
	}
	if calls > 0 {
		shape.RatePerCalls = float64(spread.Sum) / float64(calls) * ratePerCalls
	}
	for _, arm := range armSizes {
		difference := 2 * spread.SD * math.Sqrt(2/float64(arm))
		fraction := math.Inf(1)
		if spread.Mean > 0 {
			fraction = difference / spread.Mean
		}
		shape.Detectable = append(shape.Detectable, Detectable{ArmSessions: arm, Difference: difference, FractionOfMean: fraction})
	}
	return shape
}

type SessionRow struct {
	ID     string
	Calls  int
	Counts Counts
}

type Result struct {
	SessionsDir   string
	ReadAt        time.Time
	Entries       int
	Sessions      int
	Calls         int
	MutationCalls int
	Skips         []corpus.SkippedTurn
	NoWallClock   int
	CleanSessions int
	Rows          []SessionRow
	Shapes        []Shape
}

func Run(sessionsDir string, recordedBy time.Time) (Result, error) {
	live, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	walked := live.RecordedBy(recordedBy)
	result := Result{
		SessionsDir: sessionsDir,
		ReadAt:      time.Now(),
		Entries:     walked.EntryCount,
		Sessions:    len(walked.Turns),
		Skips:       walked.Skipped,
	}
	for _, session := range walked.Turns {
		if !session.WallClockRecorded {
			result.NoWallClock++
		}
		calls := callsOf(session)
		result.Calls += len(calls)
		for _, call := range calls {
			if call.Tool == "write" || call.Tool == "edit" {
				result.MutationCalls++
			}
		}
		counts := count(calls)
		if total(counts) == 0 {
			result.CleanSessions++
		}
		result.Rows = append(result.Rows, SessionRow{ID: session.ID, Calls: len(calls), Counts: counts})
	}
	sort.Slice(result.Rows, func(i, j int) bool { return result.Rows[i].ID < result.Rows[j].ID })
	pick := func(of func(Counts) int) []int {
		values := make([]int, len(result.Rows))
		for i, row := range result.Rows {
			values[i] = of(row.Counts)
		}
		return values
	}
	result.Shapes = []Shape{
		shapeOf("revert",
			"a path written or edited after it was already written or edited in the same session",
			"a planned second pass over the same file is a refactor, not a retreat",
			pick(func(c Counts) int { return c.Reverts }), result.Calls),
		shapeOf("revert, exact undo",
			"an edit whose old and new strings are an earlier edit's new and old strings",
			"removing a line added on purpose to test a hook still reads as an undo",
			pick(func(c Counts) int { return c.ExactUndos }), result.Calls),
		shapeOf("re-read",
			"a path read again over an overlapping line range with no write or edit to it between",
			"re-reading a long file after a distant tool result is how a person works, not waste",
			pick(func(c Counts) int { return c.ReReads }), result.Calls),
		shapeOf("re-read, identical range",
			"the same path, the same line range and the same result bytes, no mutation between",
			"a compaction that dropped the earlier result makes the second read necessary",
			pick(func(c Counts) int { return c.IdenticalReReads }), result.Calls),
		shapeOf("contradiction",
			"a call that failed immediately after a call that did not",
			"a failure can be the answer asked for, as when a test is expected to fail first",
			pick(func(c Counts) int { return c.Contradictions }), result.Calls),
	}
	return result, nil
}
