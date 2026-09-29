package tasks

import (
	"slices"
	"testing"

	"tofu/bench/browser/airbnb"
)

func TestEachTaskScoresItsPassingFixtureFullAndItsFailingOneLower(t *testing.T) {
	for _, recorded := range []struct {
		task, fixture string
		failed        []int
	}{
		{"books", "books-pass", nil},
		{"books", "books-fail", []int{3, 5, 9}},
		{"herokuapp", "herokuapp-pass", nil},
		{"herokuapp", "herokuapp-fail", []int{2, 3, 5}},
		{"wikipedia", "wikipedia-pass", nil},
		{"wikipedia", "wikipedia-fail", []int{1, 4, 5}},
	} {
		task, err := Named(recorded.task)
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := Read(airbnb.ArmA, "testdata/"+recorded.fixture+".jsonl")
		if err != nil {
			t.Fatal(err)
		}
		row := Score(task, evidence)
		var failed []int
		for _, result := range row.Steps {
			if !result.Passed {
				failed = append(failed, result.Step)
			}
		}
		t.Logf("%s: %d of %d, failing %v", recorded.fixture, row.Passed, len(row.Steps), failed)
		if len(row.Steps) != len(task.Steps) || !slices.Equal(failed, recorded.failed) {
			t.Errorf("%s: failing %v of %d steps, want %v", recorded.fixture, failed, len(row.Steps), recorded.failed)
		}
	}
}

func TestAnUnknownTaskIsRefused(t *testing.T) {
	if _, err := Named("airbnb-by-another-name"); err == nil {
		t.Error("an unknown task name was accepted")
	}
}
