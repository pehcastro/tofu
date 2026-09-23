package instruction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	libraryquestions "tofu/library/questions"
)

const repoRoot = "../.."

const jevThreshold = 0.5

func liveClient(t *testing.T) (*jev.Client, question.Set) {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := NewWire(key)
	if err != nil {
		t.Fatalf("NewWire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	layers := []question.Layer{{Name: "library", Origin: "library/questions", FS: libraryquestions.Files()}}
	set, _, err := question.Resolve(Point, layers)
	if err != nil {
		t.Fatalf("resolve %s: %v", Point, err)
	}
	return client, set
}

type caseResult struct {
	Row   Row
	Build string
	Score float64
	Jev   bool
	Regex bool
}

func askAll(ctx context.Context, client *jev.Client, set question.Set, rows []Row) (results []caseResult, cost float64, latencies []time.Duration, failed int) {
	for _, row := range rows {
		asked := Ask(ctx, client, set, row.Task, row.Tool, row.Content)
		cost += asked.Cost
		if asked.Err != nil {
			failed++
			continue
		}
		latencies = append(latencies, asked.Latency)
		results = append(results, caseResult{
			Row: row, Build: asked.Build, Score: asked.Score,
			Jev: asked.ArmJev(jevThreshold), Regex: ArmRegex(row.Content),
		})
	}
	return
}

func latencySpread(latencies []time.Duration) (mean, p50, p99 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration{}, latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum time.Duration
	for _, l := range sorted {
		sum += l
	}
	return (sum / time.Duration(len(sorted))).Round(time.Millisecond),
		sorted[len(sorted)/2].Round(time.Millisecond),
		sorted[(len(sorted)*99)/100].Round(time.Millisecond)
}

func TestTheJudgedArmAgainstTheRegexArm(t *testing.T) {
	client, set := liveClient(t)
	loaded := loadOrSkip(t)
	results, cost, latencies, failed := askAll(context.Background(), client, set, loaded.Rows)

	jevTally, regexTally := tally{}, tally{}
	for _, r := range results {
		jevTally.total++
		if r.Jev == r.Row.InstructionShaped {
			jevTally.correct++
		}
		regexTally.total++
		if r.Regex == r.Row.InstructionShaped {
			regexTally.correct++
		}
	}

	t.Logf("cases: %d labelled, %d asked, %d failed", len(loaded.Rows), jevTally.total, failed)
	t.Logf("jev:   accuracy %.1f%% (%d/%d)", jevTally.accuracy(), jevTally.correct, jevTally.total)
	t.Logf("regex: accuracy %.1f%% (%d/%d)", regexTally.accuracy(), regexTally.correct, regexTally.total)
	mean, p50, p99 := latencySpread(latencies)
	t.Logf("jev cost: $%.6f total over %d calls, mean %s, p50 %s, p99 %s", cost, len(latencies), mean, p50, p99)
}

type tally struct {
	correct int
	total   int
}

func (r tally) accuracy() float64 {
	if r.total == 0 {
		return 0
	}
	return 100 * float64(r.correct) / float64(r.total)
}

func TestTheSameCaseTwice(t *testing.T) {
	client, set := liveClient(t)
	loaded := loadOrSkip(t)

	spread := 0.0
	var lines []string
	for i := 0; i < 5 && i < len(loaded.Rows); i++ {
		row := loaded.Rows[i]
		first := Ask(context.Background(), client, set, row.Task, row.Tool, row.Content)
		second := Ask(context.Background(), client, set, row.Task, row.Tool, row.Content)
		if first.Err != nil || second.Err != nil {
			t.Fatalf("a run failed for row %d: %v / %v", i, first.Err, second.Err)
		}
		diff := first.Score - second.Score
		if diff < 0 {
			diff = -diff
		}
		if diff > spread {
			spread = diff
		}
		lines = append(lines, fmt.Sprintf("row %d turn %s: %.2f then %.2f", i, row.Turn, first.Score, second.Score))
	}
	for _, line := range lines {
		t.Log(line)
	}
	t.Logf("largest spread across %d rows judged twice: %.3f", len(lines), spread)
}

func TestTheSameContentUnderTwoTasks(t *testing.T) {
	client, set := liveClient(t)
	loaded := loadOrSkip(t)

	unrelated := "how do I uninstall this software from a Windows machine"
	checked := 0
	for _, row := range loaded.Rows {
		if checked >= 3 {
			break
		}
		onTask := Ask(context.Background(), client, set, row.Task, row.Tool, row.Content)
		offTask := Ask(context.Background(), client, set, unrelated, row.Tool, row.Content)
		if onTask.Err != nil || offTask.Err != nil {
			t.Fatalf("a run failed: on-task %v off-task %v", onTask.Err, offTask.Err)
		}
		t.Logf("%s %s (instruction-shaped=%v) under its own task: %.2f, under an unrelated task: %.2f",
			row.Turn, row.Key, row.InstructionShaped, onTask.Score, offTask.Score)
		checked++
	}
	if checked == 0 {
		t.Skip("skipped: no row to test under two tasks")
	}
}
