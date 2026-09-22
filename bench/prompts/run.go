package prompts

import (
	"math"
	"sort"
	"strings"
	"time"

	"tofu/bench/corpus"
	"tofu/bench/thrift"
)

const (
	headlineThresholdTokens = 10000
	armSessions             = 30
)

var thresholdSweep = []int64{1000, 2500, 5000, headlineThresholdTokens, 25000, 50000}

var leakageTerms = []string{
	"bench",
	"frugal",
	"prompt",
	"read volume",
	"revert",
	"system prompt",
	"thrift",
	"token",
	"wrong path",
	"wrong-path",
	"wrong direction",
}

type Leak struct {
	ID   string
	Term string
}

type Separable struct {
	Name       string
	Mean       float64
	SD         float64
	Difference float64
}

type Correlation struct {
	Set      string
	N        int
	Pearson  float64
	Spearman float64
}

type Result struct {
	SessionsDir   string
	ReadAt        time.Time
	Entries       int
	Skips         []corpus.SkippedTurn
	NoWallClock   int
	EmptyTask     []string
	Sessions      []Session
	DistinctTasks int
	Leaks         []Leak
	ReadTokens    thrift.Spread
	ReadCalls     thrift.Spread
	WrongPerSet   thrift.Spread
	Separables    []Separable
	Correlations  []Correlation
	Tables        []Table
}

func leakOf(task string) string {
	lowered := strings.ToLower(task)
	for _, term := range leakageTerms {
		if strings.Contains(lowered, term) {
			return term
		}
	}
	return ""
}

func separable(name string, values []int64) Separable {
	if len(values) == 0 {
		return Separable{Name: name}
	}
	entry := Separable{Name: name}
	for _, value := range values {
		entry.Mean += float64(value)
	}
	entry.Mean /= float64(len(values))
	for _, value := range values {
		entry.SD += (float64(value) - entry.Mean) * (float64(value) - entry.Mean)
	}
	entry.SD = math.Sqrt(entry.SD / float64(len(values)))
	entry.Difference = 2 * entry.SD * math.Sqrt(2/float64(armSessions))
	return entry
}

func correlate(set string, sessions []Session) Correlation {
	volume := make([]float64, len(sessions))
	wrong := make([]float64, len(sessions))
	for i, session := range sessions {
		volume[i] = float64(session.ReadTokens)
		wrong[i] = float64(session.WrongDirections())
	}
	return Correlation{
		Set:      set,
		N:        len(sessions),
		Pearson:  pearson(volume, wrong),
		Spearman: spearman(volume, wrong),
	}
}

func withoutTheWorst(sessions []Session) []Session {
	worst, id := 0, ""
	for _, session := range sessions {
		if session.WrongDirections() > worst {
			worst, id = session.WrongDirections(), session.ID
		}
	}
	kept := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if session.ID != id {
			kept = append(kept, session)
		}
	}
	return kept
}

func Run(sessionsDir string) (Result, error) {
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SessionsDir: sessionsDir,
		ReadAt:      time.Now(),
		Entries:     walked.EntryCount,
		Skips:       walked.Skipped,
	}
	tasks := map[string]bool{}
	leaked := map[string]bool{}
	for _, turn := range walked.Turns {
		if !turn.WallClockRecorded {
			result.NoWallClock++
		}
		session := measure(turn)
		task := strings.TrimSpace(session.Task)
		if task == "" {
			result.EmptyTask = append(result.EmptyTask, session.ID)
		}
		tasks[task] = true
		if term := leakOf(session.Task); term != "" {
			result.Leaks = append(result.Leaks, Leak{ID: session.ID, Term: term})
			leaked[session.ID] = true
		}
		result.Sessions = append(result.Sessions, session)
	}
	sort.Slice(result.Sessions, func(i, j int) bool { return result.Sessions[i].ID < result.Sessions[j].ID })
	result.DistinctTasks = len(tasks)

	readTokens := make([]int64, len(result.Sessions))
	readCalls := make([]int64, len(result.Sessions))
	wrong := make([]int64, len(result.Sessions))
	clean := make([]Session, 0, len(result.Sessions))
	for i, session := range result.Sessions {
		readTokens[i] = session.ReadTokens
		readCalls[i] = int64(session.ReadCalls)
		wrong[i] = int64(session.WrongDirections())
		if !leaked[session.ID] {
			clean = append(clean, session)
		}
	}
	result.ReadTokens = thrift.Measure(readTokens)
	result.ReadCalls = thrift.Measure(readCalls)
	result.WrongPerSet = thrift.Measure(wrong)
	result.Separables = []Separable{
		separable("read tokens per session", readTokens),
		separable("wrong directions per session", wrong),
	}
	result.Correlations = []Correlation{
		correlate("every session", result.Sessions),
		correlate("leakage excluded", clean),
		correlate("leakage excluded, worst wrong-direction session dropped", withoutTheWorst(clean)),
	}
	for _, threshold := range thresholdSweep {
		result.Tables = append(result.Tables, cross(result.Sessions, threshold))
	}
	return result, nil
}
