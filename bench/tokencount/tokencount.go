package tokencount

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/konst"
)

type Shape string

const (
	ShapeProse        Shape = "prose, the assistant's own text"
	ShapeToolCallArgs Shape = "tool-call arguments, json shaped by the tool's schema"
)

type Sample struct {
	Turn        string
	Step        int
	Wire        string
	Shape       Shape
	Bytes       int
	Actual      int
	Estimate    int
	ErrorPct    float64
	Accountable bool
}

const (
	AccountableLabel   = "bytes >= billed tokens, a tokenizer could produce this count from what's on record"
	UnaccountableLabel = "bytes < billed tokens, no tokenizer can produce more tokens than there are bytes"
)

type SkippedStep struct {
	Turn   string
	Step   int
	Reason string
}

type Result struct {
	SessionsDir          string
	Entries              int
	Turns                int
	TurnsSkipped         []corpus.SkippedTurn
	TurnsNoWire          int
	StepsRead            int
	StepsUsable          int
	StepsSkipped         []SkippedStep
	Samples              []Sample
	MessagesScanned      int
	MessagesWithThinking int
	TurnsScannedForThink int
}

type header struct {
	Wire string `json:"wire"`
}

type messageLine struct {
	Kind string `json:"kind"`
	Body struct {
		Thinking  string          `json:"thinking"`
		Reasoning json.RawMessage `json:"reasoning"`
	} `json:"body"`
}

func scanThinking(sessionsDir string, t corpus.Turn) (total, withThinking int, err error) {
	if t.Schema != corpus.SchemaHeaderJSONL {
		return 0, 0, nil
	}
	body, err := os.ReadFile(filepath.Join(sessionsDir, t.ID, "body.jsonl"))
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var entry messageLine
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Kind != "message" {
			continue
		}
		total++
		if entry.Body.Thinking != "" || (len(entry.Body.Reasoning) > 0 && string(entry.Body.Reasoning) != "null") {
			withThinking++
		}
	}
	return total, withThinking, nil
}

func readWire(sessionsDir string, t corpus.Turn) (string, error) {
	path := filepath.Join(sessionsDir, t.ID+".json")
	if t.Schema == corpus.SchemaHeaderJSONL {
		path = filepath.Join(sessionsDir, t.ID, "header.json")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var h header
	if err := json.Unmarshal(body, &h); err != nil {
		return "", err
	}
	return h.Wire, nil
}

func measureStep(turnID, wire string, step corpus.RecordedStep) (Sample, string) {
	hasText := step.AssistantText != ""
	hasCalls := len(step.ToolCalls) > 0
	switch {
	case hasText && hasCalls:
		return Sample{}, "step mixes assistant text and tool calls; the completion receipt covers both and cannot be split by shape"
	case !hasText && !hasCalls:
		return Sample{}, "step carries neither assistant text nor a tool call"
	}
	shape := ShapeProse
	bytesN := len(step.AssistantText)
	if hasCalls {
		shape = ShapeToolCallArgs
		bytesN = 0
		for _, call := range step.ToolCalls {
			bytesN += len(call.Args)
		}
		if bytesN == 0 {
			return Sample{}, "tool-call step carries no Args bytes to measure"
		}
	}
	if step.CompletionTokens <= 0 {
		return Sample{}, "step reports no completion_tokens to compare against"
	}
	estimate := bytesN / konst.SearchBytesPerToken
	errPct := math.Abs(float64(estimate-step.CompletionTokens)) / float64(step.CompletionTokens) * 100
	return Sample{
		Turn: turnID, Step: step.Index, Wire: wire, Shape: shape,
		Bytes: bytesN, Actual: step.CompletionTokens, Estimate: estimate, ErrorPct: errPct,
		Accountable: bytesN >= step.CompletionTokens,
	}, ""
}

func accountableLabel(s Sample) string {
	if s.Accountable {
		return AccountableLabel
	}
	return UnaccountableLabel
}

func Run(sessionsDir string) (Result, error) {
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SessionsDir:  sessionsDir,
		Entries:      walked.EntryCount,
		Turns:        len(walked.Turns),
		TurnsSkipped: walked.Skipped,
	}
	for _, turn := range walked.Turns {
		if total, withThinking, err := scanThinking(sessionsDir, turn); err == nil && total > 0 {
			result.TurnsScannedForThink++
			result.MessagesScanned += total
			result.MessagesWithThinking += withThinking
		}
		wire, err := readWire(sessionsDir, turn)
		switch {
		case err != nil:
			skipAllSteps(&result, turn, fmt.Sprintf("could not re-read the turn's own wire field: %v", err))
			continue
		case wire == "":
			result.TurnsNoWire++
			skipAllSteps(&result, turn, "turn carries no wire field, recorded before it existed")
			continue
		}
		for _, step := range turn.Steps {
			result.StepsRead++
			sample, reason := measureStep(turn.ID, wire, step)
			if reason != "" {
				result.StepsSkipped = append(result.StepsSkipped, SkippedStep{Turn: turn.ID, Step: step.Index, Reason: reason})
				continue
			}
			result.StepsUsable++
			result.Samples = append(result.Samples, sample)
		}
	}
	return result, nil
}

func skipAllSteps(result *Result, turn corpus.Turn, reason string) {
	for _, step := range turn.Steps {
		result.StepsRead++
		result.StepsSkipped = append(result.StepsSkipped, SkippedStep{Turn: turn.ID, Step: step.Index, Reason: reason})
	}
}

type Stats struct {
	Key         string
	N           int
	MedianPct   float64
	WorstPct    float64
	ShareOver10 float64
}

func statsOf(key string, samples []Sample) Stats {
	if len(samples) == 0 {
		return Stats{Key: key}
	}
	errs := make([]float64, len(samples))
	over10 := 0
	worst := 0.0
	for i, s := range samples {
		errs[i] = s.ErrorPct
		if s.ErrorPct > worst {
			worst = s.ErrorPct
		}
		if s.ErrorPct > 10 {
			over10++
		}
	}
	sort.Float64s(errs)
	return Stats{
		Key:         key,
		N:           len(errs),
		MedianPct:   median(errs),
		WorstPct:    worst,
		ShareOver10: 100 * float64(over10) / float64(len(errs)),
	}
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func ByWire(samples []Sample) []Stats {
	return statsRows(groupBy(samples, func(s Sample) string { return s.Wire }))
}

func ByShape(samples []Sample) []Stats {
	return statsRows(groupBy(samples, func(s Sample) Shape { return s.Shape }))
}

func ByAccountable(samples []Sample) []Stats {
	return statsRows(groupBy(samples, accountableLabel))
}

func Filter(samples []Sample, keep func(Sample) bool) []Sample {
	var out []Sample
	for _, s := range samples {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

func groupBy[K comparable](samples []Sample, key func(Sample) K) map[K][]Sample {
	grouped := map[K][]Sample{}
	for _, s := range samples {
		grouped[key(s)] = append(grouped[key(s)], s)
	}
	return grouped
}

func statsRows[K ~string](grouped map[K][]Sample) []Stats {
	var out []Stats
	for k, group := range grouped {
		out = append(out, statsOf(string(k), group))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
