package readworth

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

func TestTheJudgedArmAgainstTheFreeArmsOverTheHandLabelledCorpus(t *testing.T) {
	client, set := liveClient(t)
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 2; run++ {
		var latencies []time.Duration
		cost, failed := 0.0, 0
		started := time.Now()

		jevMarks := make([]sift.Mark, len(rows))
		for i, row := range rows {
			asked := Ask(context.Background(), client, set, row)
			jevMarks[i] = asked.ArmJev()
			if asked.Err != nil {
				failed++
			} else {
				latencies = append(latencies, asked.Latency)
			}
			cost += asked.Cost
		}

		jevResult := tally(rows, jevMarks)
		keepAll := tally(rows, marksFor(rows, ArmKeepEverything))
		signpost := tally(rows, marksFor(rows, ArmSignpost))

		t.Logf("run %d jev:             accuracy %.1f%%, saved %.1f%%", run, jevResult.accuracy(), jevResult.savedPct())
		t.Logf("run %d keep everything: accuracy %.1f%%, saved %.1f%%", run, keepAll.accuracy(), keepAll.savedPct())
		t.Logf("run %d signpost:        accuracy %.1f%%, saved %.1f%%", run, signpost.accuracy(), signpost.savedPct())
		logLengthSweep(t, rows, fmt.Sprintf("run %d ", run))

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		if len(latencies) > 0 {
			t.Logf("run %d jev arm: %d rows, %d failed, $%.6f total, mean %s, p50 %s, p99 %s, %s of wall clock",
				run, len(rows), failed, cost,
				(sum / time.Duration(len(latencies))).Round(time.Millisecond),
				latencies[len(latencies)/2].Round(time.Millisecond),
				latencies[(len(latencies)*99)/100].Round(time.Millisecond),
				time.Since(started).Round(time.Millisecond))
		}
	}
}

func TestTheSameRowJudgedTwice(t *testing.T) {
	client, set := liveClient(t)
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatal(err)
	}

	spread := 0.0
	var lines []string
	for i := 0; i < 5 && i < len(rows); i++ {
		row := rows[i]
		first := Ask(context.Background(), client, set, row)
		second := Ask(context.Background(), client, set, row)
		if first.Err != nil || second.Err != nil {
			t.Fatalf("a run failed for row %d: %v / %v", i, first.Err, second.Err)
		}
		diff := math.Abs(first.Scores["answers_the_task"] - second.Scores["answers_the_task"])
		if diff > spread {
			spread = diff
		}
		lines = append(lines, fmt.Sprintf("row %d turn %s: %.2f then %.2f", i, row.Turn, first.Scores["answers_the_task"], second.Scores["answers_the_task"]))
	}
	for _, line := range lines {
		t.Log(line)
	}
	t.Logf("largest answers_the_task spread across 5 rows judged twice: %.3f", spread)
}

func TestTheSameParagraphUnderTwoTasks(t *testing.T) {
	client, set := liveClient(t)
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	var withContent Row
	for _, row := range rows {
		if row.Keep {
			withContent = row
			break
		}
	}
	if withContent.Turn == "" {
		t.Fatal("no kept row in the corpus to judge under two tasks")
	}

	unrelated := "how do I uninstall this software from a Windows machine"
	original := Ask(context.Background(), client, set, withContent)
	off := withContent
	off.Task = unrelated
	shifted := Ask(context.Background(), client, set, off)
	if original.Err != nil || shifted.Err != nil {
		t.Fatalf("a run failed: original %v shifted %v", original.Err, shifted.Err)
	}

	onTask := original.Scores["answers_the_task"]
	offTask := shifted.Scores["answers_the_task"]
	t.Logf("paragraph from turn %s under %q: %.2f, under %q: %.2f", withContent.Turn, withContent.Task, onTask, unrelated, offTask)
	if onTask-offTask < 0.5 {
		t.Fatalf("one paragraph scored %.2f and %.2f under two tasks, so the answer is close to a function of the paragraph's own characters", onTask, offTask)
	}
}
