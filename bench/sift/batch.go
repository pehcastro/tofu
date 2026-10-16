package sift

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/bench/stat"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/konst"
	"tofu/internal/sift"
)

type Arm string

const (
	ArmPerChunk      Arm = "per-chunk, as shipped"
	ArmBatched       Arm = "batched, rubric per question"
	ArmBatchedRubric Arm = "batched, rubric in state"
	ArmFree          Arm = "free head/tail regex"
)

var JevArms = []Arm{ArmPerChunk, ArmBatched, ArmBatchedRubric}

const (
	beforePerChunkNeedles = 27
	beforeFreeNeedles     = 18
	beforeDollars         = 0.00027
	beforeWallMS          = 838
)

type BatchChunk struct {
	Stream sift.Stream `json:"stream"`
	Text   string      `json:"text"`
}

type Rubric struct {
	Instructions string `json:"instructions"`
	True         string `json:"true"`
	False        string `json:"false"`
}

type BatchState struct {
	Task     string       `json:"task"`
	Command  string       `json:"command"`
	ExitCode int          `json:"exit_code"`
	Chunks   []BatchChunk `json:"chunks"`
	Rubric   *Rubric      `json:"rubric,omitempty"`
}

type Batch struct {
	Request        jev.Request
	Indexes        []int
	RequestTokens  int
	QuestionTokens int
}

func BuildBatch(arm Arm, shipped jev.Question, model string, shell sift.Shell, units []sift.Unit, task string) (Batch, error) {
	state := BatchState{Task: task, Command: shell.Command, ExitCode: shell.ExitCode}
	for _, unit := range units {
		state.Chunks = append(state.Chunks, BatchChunk{Stream: unit.Stream, Text: strings.TrimRight(unit.Text, "\n")})
	}
	if arm == ArmBatchedRubric {
		state.Rubric = &Rubric{Instructions: shipped.Instructions, True: shipped.True, False: shipped.False}
	}
	batch := Batch{Request: jev.Request{State: state}}
	for _, unit := range units {
		if unit.Held != sift.NotHeld {
			continue
		}
		question := chunkQuestion(arm, shipped, unit.Index)
		alone, err := jev.Request{State: state, Questions: []jev.Question{question}}.Encode(model)
		if err != nil {
			return Batch{}, err
		}
		batch.QuestionTokens = max(batch.QuestionTokens, jev.EstimateTokens(len(alone)))
		batch.Request.Questions = append(batch.Request.Questions, question)
		batch.Indexes = append(batch.Indexes, unit.Index)
	}
	if len(batch.Indexes) == 0 {
		return batch, nil
	}
	body, err := batch.Request.Encode(model)
	if err != nil {
		return Batch{}, err
	}
	batch.RequestTokens = jev.EstimateTokens(len(body))
	if batch.RequestTokens > konst.JudgeRequestTokenCeiling || batch.QuestionTokens > konst.JudgeStateTokenCeiling {
		return Batch{}, fmt.Errorf("%q: the request is about %d tokens and its largest question %d, over %d and %d", shell.Command, batch.RequestTokens, batch.QuestionTokens, konst.JudgeRequestTokenCeiling, konst.JudgeStateTokenCeiling)
	}
	return batch, nil
}

func chunkQuestion(arm Arm, shipped jev.Question, index int) jev.Question {
	chunk := fmt.Sprintf("chunks[%d]", index)
	question := jev.Question{ID: fmt.Sprintf("%s_%d", sift.NeededQuestion, index), Kind: jev.QuestionNoul}
	switch arm {
	case ArmBatched:
		question.Instructions = shipped.Instructions + " The chunk this question asks about is " + chunk + " in the state; the other chunks are there for context only."
		question.True, question.False = shipped.True, shipped.False
	case ArmBatchedRubric:
		question.Instructions = "Apply the rubric in the state to " + chunk + " and to no other chunk."
		question.True = "rubric.true holds for " + chunk + "."
		question.False = "rubric.false holds for " + chunk + "."
	default:
		panic("sift: no batched question for arm " + string(arm))
	}
	return question
}

type Asked struct {
	Arm            Arm             `json:"arm"`
	Run            int             `json:"run"`
	Row            int             `json:"row"`
	Wire           string          `json:"wire"`
	Builds         []string        `json:"builds"`
	Requests       int             `json:"requests"`
	Errors         int             `json:"errors"`
	Cost           float64         `json:"cost"`
	WallMS         int64           `json:"wall_ms"`
	InputTokens    int             `json:"input_tokens"`
	EstimateTokens int             `json:"estimate_tokens"`
	QuestionTokens int             `json:"question_tokens"`
	Scores         map[int]float64 `json:"scores"`
	Failure        string          `json:"failure,omitempty"`
}

func AskArm(ctx context.Context, asker Asker, arm Arm, planted Planted, task string) (Asked, error) {
	if arm == ArmPerChunk {
		started := time.Now()
		answered := asker.Ask(ctx, planted.Shell, planted.Units, task)
		return Asked{
			Arm:         arm,
			Wire:        asker.Client.Caps().Name,
			Builds:      slices.Sorted(maps.Keys(answered.Builds)),
			Requests:    len(answered.Scores) + answered.Errors,
			Errors:      answered.Errors,
			Cost:        answered.Cost,
			WallMS:      time.Since(started).Milliseconds(),
			InputTokens: answered.InputTokens,
			Scores:      answered.Scores,
		}, nil
	}
	needed, ok := asker.Set.Question(sift.NeededQuestion)
	if !ok {
		return Asked{}, fmt.Errorf("the shipped set carries no %q", sift.NeededQuestion)
	}
	batch, err := BuildBatch(arm, needed.ToJev(), asker.Client.Model(), planted.Shell, planted.Units, task)
	if err != nil {
		return Asked{}, err
	}
	asked := Asked{Arm: arm, Wire: asker.Client.Caps().Name, Scores: map[int]float64{}, EstimateTokens: batch.RequestTokens, QuestionTokens: batch.QuestionTokens}
	if len(batch.Indexes) == 0 {
		return asked, nil
	}
	asked.Requests = 1
	started := time.Now()
	decision, err := asker.Client.Ask(ctx, batch.Request)
	asked.WallMS = time.Since(started).Milliseconds()
	if err != nil {
		asked.Errors, asked.Failure = 1, err.Error()
		return asked, nil
	}
	asked.Builds = []string{decision.Build}
	asked.Cost = decision.Usage.Cost
	asked.InputTokens = decision.Usage.InputTokens
	for i, index := range batch.Indexes {
		asked.Scores[index] = decision.Answers[batch.Request.Questions[i].ID].Noul
	}
	return asked, nil
}

func WriteAsked(path string, asked []Asked) error {
	var out strings.Builder
	for _, a := range asked {
		line, err := json.Marshal(a)
		if err != nil {
			return err
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func ReadAsked(path string) ([]Asked, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var asked []Asked
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var a Asked
		if err := json.Unmarshal([]byte(line), &a); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, len(asked)+1, err)
		}
		asked = append(asked, a)
	}
	return asked, nil
}

type armRun struct {
	arm Arm
	run int
}

func BatchTable(rows []Row, asked []Asked, keepAt float64) (string, error) {
	plants := make([]Planted, len(rows))
	freeKept := 0
	for i, row := range rows {
		planted, err := Plant(row, i)
		if err != nil {
			return "", err
		}
		plants[i] = planted
		if Free(row, planted).NeedleKept {
			freeKept++
		}
	}

	byRun := map[armRun][]Asked{}
	builds, wires := map[string]int{}, map[string]int{}
	runs, largestRequest, largestEstimate, largestQuestion, spent := 0, 0, 0, 0, 0.0
	for _, a := range asked {
		if a.Errors > 0 {
			return "", fmt.Errorf("%s run %d row %d: %d of %d requests failed, and a failed request keeps every chunk, so the needles would be counted kept for nothing", a.Arm, a.Run, a.Row, a.Errors, a.Requests)
		}
		dollars, err := billed(a)
		if err != nil {
			return "", err
		}
		a.Cost = dollars
		spent += dollars
		byRun[armRun{a.Arm, a.Run}] = append(byRun[armRun{a.Arm, a.Run}], a)
		runs = max(runs, a.Run)
		wires[a.Wire]++
		for _, build := range a.Builds {
			builds[build]++
		}
		if a.Arm != ArmPerChunk {
			largestRequest = max(largestRequest, a.InputTokens)
			largestEstimate = max(largestEstimate, a.EstimateTokens)
			largestQuestion = max(largestQuestion, a.QuestionTokens)
		}
	}
	if len(builds) != 1 || len(wires) != 1 {
		return "", fmt.Errorf("the run reports builds %v on wires %v, and arms are compared on one build and one wire only", builds, wires)
	}
	if largestRequest > konst.JudgeRequestTokenCeiling || largestEstimate > konst.JudgeRequestTokenCeiling || largestQuestion > konst.JudgeStateTokenCeiling {
		return "", fmt.Errorf("a batched request reached %d tokens reported, %d estimated, with a question of %d, over %d and %d", largestRequest, largestEstimate, largestQuestion, konst.JudgeRequestTokenCeiling, konst.JudgeStateTokenCeiling)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "builds reported: %v; wire %v; total spent $%.6f\n\n", builds, slices.Collect(maps.Keys(wires)), spent)
	out.WriteString("before: openrouter, typesafe/jev-1.13-20260917, bench/report/answers.json and bench/sift/report-2026-09-22.md\ndollars: usage.cost on openrouter; typesafe reports usage.cost 0, so input tokens times $42 per billion\n\n")
	out.WriteString("wall: per shell result that sent at least one request\n\n")
	fmt.Fprintf(&out, "| arm | run | needles kept of %d | before | $ per shell result | before | wall p50 ms | wall p95 ms | wall worst ms | before | results asked | requests |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n", len(rows))
	for _, arm := range JevArms {
		for run := 1; run <= runs; run++ {
			results := byRun[armRun{arm, run}]
			if len(results) != len(rows) {
				return "", fmt.Errorf("%s run %d carries %d shell results of %d", arm, run, len(results), len(rows))
			}
			kept, cost, requests := 0, 0.0, 0
			var walls []float64
			for _, a := range results {
				if Read(rows[a.Row], plants[a.Row], Answered{Units: plants[a.Row].Units, Scores: a.Scores}.Cut(keepAt)).NeedleKept {
					kept++
				}
				cost += a.Cost
				requests += a.Requests
				if a.Requests > 0 {
					walls = append(walls, float64(a.WallMS))
				}
			}
			_, worst := stat.Spread(walls)
			beforeNeedles, beforeCost, beforeWall := "-", "-", "-"
			if arm == ArmPerChunk {
				beforeNeedles, beforeCost, beforeWall = fmt.Sprint(beforePerChunkNeedles), fmt.Sprintf("$%.5f", beforeDollars), fmt.Sprint(beforeWallMS)
			}
			fmt.Fprintf(&out, "| %s | %d | %d | %s | $%.6f | %s | %.0f | %.0f | %.0f | %s | %d | %d |\n",
				arm, run, kept, beforeNeedles, cost/float64(len(rows)), beforeCost,
				stat.Median(walls), stat.Percentile(walls, 95), worst, beforeWall, len(walls), requests)
		}
	}
	fmt.Fprintf(&out, "| %s | - | %d | %d | $0 | - | - | - | - | - | 0 | 0 |\n\n", ArmFree, freeKept, beforeFreeNeedles)
	out.WriteString(flipTable(byRun, runs, keepAt))
	fmt.Fprintf(&out, "\nlargest batched request: %d input tokens reported by usage, %d estimated before sending; largest single question %d estimated; ceilings %d and %d\n",
		largestRequest, largestEstimate, largestQuestion, konst.JudgeRequestTokenCeiling, konst.JudgeStateTokenCeiling)
	return out.String(), nil
}

func billed(a Asked) (float64, error) {
	switch a.Wire {
	case openrouter.Name:
		return a.Cost, nil
	case typesafe.Name:
		return float64(a.InputTokens) * typesafe.DollarsPerInputToken, nil
	}
	return 0, fmt.Errorf("%s run %d row %d: no price for wire %q", a.Arm, a.Run, a.Row, a.Wire)
}

func flipTable(byRun map[armRun][]Asked, runs int, keepAt float64) string {
	reference := map[int]map[int]float64{}
	for _, a := range byRun[armRun{ArmPerChunk, 1}] {
		reference[a.Row] = a.Scores
	}
	var out strings.Builder
	fmt.Fprintf(&out, "| against %s run 1, keep_at %.2f | run | chunks flipped | kept to cut | cut to kept | chunks compared |\n|---|---|---|---|---|---|\n", ArmPerChunk, keepAt)
	for _, arm := range JevArms {
		for run := 1; run <= runs; run++ {
			if arm == ArmPerChunk && run == 1 {
				continue
			}
			toCut, toKept, compared := 0, 0, 0
			for _, a := range byRun[armRun{arm, run}] {
				for index, score := range reference[a.Row] {
					other, ok := a.Scores[index]
					if !ok {
						continue
					}
					compared++
					switch {
					case score >= keepAt && other < keepAt:
						toCut++
					case score < keepAt && other >= keepAt:
						toKept++
					}
				}
			}
			fmt.Fprintf(&out, "| %s | %d | %d | %d | %d | %d |\n", arm, run, toCut+toKept, toCut, toKept, compared)
		}
	}
	return out.String()
}
