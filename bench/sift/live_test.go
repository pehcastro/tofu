package sift

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/sift"
	libraryquestions "tofu/library/questions"
)

const repoRoot = "../.."

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

func tally(t *testing.T, arm string, readings []Reading) {
	t.Helper()
	kept, before, after := 0, 0, 0
	var lost []string
	for i, reading := range readings {
		before, after = before+reading.Before, after+reading.After
		if reading.NeedleKept {
			kept++
			continue
		}
		lost = append(lost, fmt.Sprintf("row %d unit %d", i, reading.NeedleAt))
	}
	t.Logf("%s: %d of %d needles kept, %d bytes to %d, %.1f%% saved, lost at %s",
		arm, kept, len(readings), before, after, 100*float64(before-after)/float64(before), strings.Join(lost, ", "))
}

func TestTheJudgedArmAgainstTheFreeArmOverEveryCapturedOutput(t *testing.T) {
	client, set := liveClient(t)
	pol := shippedRule(t)
	rows := corpusRows(t)

	for run := 1; run <= 2; run++ {
		var free, judged []Reading
		var latencies []time.Duration
		var answered []Answered
		cost, errors, calls := 0.0, 0, 0
		started := time.Now()

		for i, row := range rows {
			planted, err := Plant(row, i)
			if err != nil {
				t.Fatalf("Plant: %v", err)
			}
			asked := Ask(context.Background(), client, set, row, planted)
			answered = append(answered, asked)
			free = append(free, Free(row, planted))
			judged = append(judged, asked.Cut(pol.KeepAt))
			latencies = append(latencies, asked.Latencies...)
			cost += asked.Cost
			errors += asked.Errors
			calls += len(asked.Scores) + asked.Errors
			if run == 1 {
				t.Logf("free   %s", free[i].Line())
				t.Logf("judged %s cost $%.6f errors %d", judged[i].Line(), asked.Cost, asked.Errors)
			}
		}

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		tally(t, fmt.Sprintf("run %d free   arm, no threshold to set", run), free)
		tally(t, fmt.Sprintf("run %d judged arm at the rule's keep_at %.2f", run, pol.KeepAt), judged)
		t.Logf("run %d judged arm: %d calls, %d answered, %d failed, $%.6f, mean %s, p50 %s, p99 %s, %s of wall clock at concurrency %d",
			run, calls, len(latencies), errors, cost,
			(sum / time.Duration(len(latencies))).Round(time.Millisecond),
			latencies[len(latencies)/2].Round(time.Millisecond),
			latencies[(len(latencies)*99)/100].Round(time.Millisecond),
			time.Since(started).Round(time.Millisecond), konst.SiftConcurrency)

		for _, cut := range []float64{0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9} {
			var at []Reading
			for _, asked := range answered {
				at = append(at, asked.Cut(cut))
			}
			tally(t, fmt.Sprintf("run %d judged at keep_at %.2f", run, cut), at)
		}
	}
}

func TestTheSameChunkUnderTwoTasks(t *testing.T) {
	client, set := liveClient(t)
	chunk := "bench/cost/report-2026-09-19.md\nbench/cost/rescore-2026-09-19.md\nbench/cost/sweep-2026-09-19.md\nbench/harness/plan.go\nbench/harness/row.go\n"
	result := sift.Shell{Command: "git ls-files bench", Stdout: "the first line\n\n" + chunk + "\nthe last line\n"}
	units := sift.SplitShell(result)
	at := -1
	for _, unit := range units {
		if unit.Text == chunk+"\n" || unit.Text == chunk {
			at = unit.Index
		}
	}
	if at < 0 {
		t.Fatalf("the chunk did not survive splitting: %+v", units)
	}

	questions := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}
	scores := map[string]float64{}
	for _, task := range []string{
		"which files under bench carry a dated report?",
		"how many goroutines does the turn loop start?",
	} {
		state, err := json.Marshal(sift.BuildShellState(result, units, at, task))
		if err != nil {
			t.Fatal(err)
		}
		decision, err := client.Ask(context.Background(), jev.Request{State: json.RawMessage(state), Questions: questions})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		scores[task] = decision.Answers[sift.NeededQuestion].Noul
		t.Logf("task %q: %s %.2f", task, sift.NeededQuestion, scores[task])
	}

	asked, unrelated := scores["which files under bench carry a dated report?"], scores["how many goroutines does the turn loop start?"]
	if asked-unrelated < 0.25 {
		t.Fatalf("one chunk scored %.2f and %.2f under two tasks, so the answer is close to a function of the chunk's own characters", asked, unrelated)
	}
}
