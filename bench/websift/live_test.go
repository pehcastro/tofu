package websift

import (
	"context"
	"fmt"
	"math"
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

type groupRun struct {
	answered    []Answered
	readability []Reading
	keepAll     []Reading
	judged      []Reading
}

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

func TestTheJudgedArmAgainstTheFreeArmsOverEveryFetchedPage(t *testing.T) {
	client, set := liveClient(t)
	pol := shippedRule(t)
	rows := corpusRows(t)

	for run := 1; run <= 2; run++ {
		var latencies []time.Duration
		cost, failedChunks, totalChunks := 0.0, 0, 0
		started := time.Now()
		byGroup := map[string]*groupRun{GroupFocused: {}, GroupReference: {}}

		for i, row := range rows {
			planted, err := Plant(row, i)
			if err != nil {
				t.Fatalf("Plant: %v", err)
			}
			asked := Ask(context.Background(), client, set, row, planted)
			judgedReading := asked.Cut(pol.KeepAt)
			g := byGroup[row.Group]
			g.answered = append(g.answered, asked)
			g.readability = append(g.readability, Free(row, planted))
			g.keepAll = append(g.keepAll, KeepEverything(row, planted))
			g.judged = append(g.judged, judgedReading)
			totalChunks += asked.Chunks
			failedChunks += asked.Failed
			latencies = append(latencies, asked.Latencies...)
			cost += asked.Cost
			if run == 1 {
				t.Logf("%-9.9s readability %s", row.Group, g.readability[len(g.readability)-1].Line())
				t.Logf("%-9.9s judged      %s chunks %d cost $%.6f", row.Group, judgedReading.Line(), asked.Chunks, asked.Cost)
			}
		}

		for _, group := range []string{GroupFocused, GroupReference} {
			g := byGroup[group]
			tally(t, fmt.Sprintf("run %d %s readability arm", run, group), g.readability)
			tally(t, fmt.Sprintf("run %d %s keep everything arm", run, group), g.keepAll)
			tally(t, fmt.Sprintf("run %d %s judged arm at keep_at %.2f", run, group, pol.KeepAt), g.judged)
			for _, keepAt := range []float64{0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9} {
				var at []Reading
				for _, asked := range g.answered {
					at = append(at, asked.Cut(keepAt))
				}
				tally(t, fmt.Sprintf("run %d %s judged at keep_at %.2f", run, group, keepAt), at)
			}
		}

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		if len(latencies) > 0 {
			t.Logf("run %d judged arm: %d pages, %d chunks, %d failed, $%.6f total, mean %s, p50 %s, p99 %s, %s of wall clock",
				run, len(rows), totalChunks, failedChunks, cost,
				(sum / time.Duration(len(latencies))).Round(time.Millisecond),
				latencies[len(latencies)/2].Round(time.Millisecond),
				latencies[(len(latencies)*99)/100].Round(time.Millisecond),
				time.Since(started).Round(time.Millisecond))
		}
	}
}

func TestTheSamePageJudgedTwice(t *testing.T) {
	client, set := liveClient(t)
	rows := corpusRows(t)
	planted, err := Plant(rows[0], 0)
	if err != nil {
		t.Fatalf("Plant: %v", err)
	}

	first := Ask(context.Background(), client, set, rows[0], planted)
	second := Ask(context.Background(), client, set, rows[0], planted)
	if first.Failed > 0 || second.Failed > 0 {
		t.Fatalf("a run failed: first %d second %d", first.Failed, second.Failed)
	}

	spread := 0.0
	for i, score := range first.Scores {
		other, ok := second.Scores[i]
		if !ok {
			continue
		}
		if diff := math.Abs(score - other); diff > spread {
			spread = diff
		}
	}
	t.Logf("%s judged twice: %d units, largest still_needed spread %.3f", rows[0].Source, len(planted.Units), spread)
}

func TestTheSameUnitUnderTwoTasks(t *testing.T) {
	client, set := liveClient(t)
	rows := corpusRows(t)
	var withUnits Row
	for _, row := range rows {
		if row.Source == "jsonlines" {
			withUnits = row
			break
		}
	}
	if withUnits.Source == "" {
		withUnits = rows[0]
	}

	planted, err := Plant(withUnits, 0)
	if err != nil {
		t.Fatalf("Plant: %v", err)
	}

	scores := map[string]float64{}
	for _, task := range []string{withUnits.Task, "how do I uninstall this software from a Windows machine"} {
		row := withUnits
		row.Task = task
		asked := Ask(context.Background(), client, set, row, planted)
		if asked.Failed > 0 {
			t.Fatalf("Ask failed for task %q", task)
		}
		if score, ok := asked.Scores[planted.At]; ok {
			scores[task] = score
		}
	}
	asked, unrelated := scores[withUnits.Task], scores["how do I uninstall this software from a Windows machine"]
	t.Logf("needle unit under %q: %.2f, under an unrelated task: %.2f", withUnits.Task, asked, unrelated)
	if asked-unrelated < 0.25 {
		t.Fatalf("one unit scored %.2f and %.2f under two tasks, so the answer is close to a function of the unit's own characters", asked, unrelated)
	}
}
