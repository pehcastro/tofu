package airbnb

import (
	"slices"
	"testing"
	"time"
)

var drawnOn = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestTheSameSeedDrawsTheSameTaskAndAnotherSeedAnotherOne(t *testing.T) {
	one, again, two := Draw(1, drawnOn), Draw(1, drawnOn), Draw(2, drawnOn)
	t.Logf("seed 1: %s\nseed 2: %s", one.Prompt, two.Prompt)
	if one.Prompt != again.Prompt {
		t.Error("seed 1 drew two different prompts")
	}
	if one.Prompt == two.Prompt {
		t.Error("seeds 1 and 2 drew the same prompt")
	}
}

func TestADrawnTaskScoresItsPassingRunFullAndItsFailingOneLower(t *testing.T) {
	task := Draw(7, drawnOn)
	t.Log(task.Prompt)
	for _, recorded := range []struct {
		dir    string
		failed []int
	}{
		{"testdata/drawn-pass", nil},
		{"testdata/drawn-fail", []int{2, 4, 8}},
	} {
		run, err := LoadRun(recorded.dir)
		if err != nil {
			t.Fatal(err)
		}
		row := Score(task, run)
		var failed []int
		for _, result := range row.Steps {
			if !result.Passed {
				failed = append(failed, result.Step)
			}
		}
		t.Logf("%s: %d of %d, failing %v", recorded.dir, row.Passed, len(row.Steps), failed)
		if len(row.Steps) != 12 || !slices.Equal(failed, recorded.failed) || row.Seed != 7 {
			t.Errorf("%s: failing %v of %d with seed %d, want %v with seed 7", recorded.dir, failed, len(row.Steps), row.Seed, recorded.failed)
		}
	}
}
