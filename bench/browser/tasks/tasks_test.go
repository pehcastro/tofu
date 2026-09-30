package tasks

import (
	"slices"
	"testing"
	"time"

	"tofu/bench/browser/airbnb"
)

var drawnOn = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

const fixtureSeed = 3

func TestEachTaskScoresItsPassingFixtureFullAndItsFailingOneLower(t *testing.T) {
	for _, recorded := range []struct {
		task, fixture string
		failed        []int
	}{
		{"books", "books-pass", nil},
		{"books", "books-fail", []int{3, 5, 9}},
		{"herokuapp", "herokuapp-pass", nil},
		{"herokuapp", "herokuapp-fail", []int{2, 3, 5}},
		{"herokuapp", "herokuapp-delta", nil},
		{"herokuapp", "herokuapp-noafter", []int{3}},
		{"herokuapp", "herokuapp-star", nil},
		{"wikipedia", "wikipedia-pass", nil},
		{"wikipedia", "wikipedia-fail", []int{1, 4, 5}},
		{"wikipedia", "wikipedia-redirect", nil},
		{"wikipedia", "wikipedia-lead", nil},
		{"flights", "flights-pass", nil},
		{"flights", "flights-fail", []int{1, 4, 6}},
		{"flights", "flights-china", nil},
		{"flights", "flights-absent", []int{6}},
		{"youtube", "youtube-pass", nil},
		{"youtube", "youtube-fail", []int{2, 5}},
	} {
		task, err := Named(recorded.task, fixtureSeed, drawnOn)
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

func TestTheSameSeedDrawsTheSameFlightAndAnotherSeedAnotherOne(t *testing.T) {
	prompt := func(seed int64) string {
		task, err := Named("flights", seed, drawnOn)
		if err != nil {
			t.Fatal(err)
		}
		return task.Prompt
	}
	t.Logf("seed 1: %s\nseed 2: %s\nseed %d: %s", prompt(1), prompt(2), fixtureSeed, prompt(fixtureSeed))
	if prompt(1) != prompt(1) || prompt(1) == prompt(2) {
		t.Error("seed 1 did not draw one flight, or seeds 1 and 2 drew the same one")
	}
	youtube, err := Named("youtube", fixtureSeed, drawnOn)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("youtube seed %d: %s", fixtureSeed, youtube.Prompt)
}

func TestAnUnknownTaskIsRefused(t *testing.T) {
	if _, err := Named("airbnb-by-another-name", 0, drawnOn); err == nil {
		t.Error("an unknown task name was accepted")
	}
}
