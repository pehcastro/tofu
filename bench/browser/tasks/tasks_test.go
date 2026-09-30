package tasks

import (
	"slices"
	"testing"
	"time"

	"tofu/bench/browser/airbnb"
)

var drawnOn = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

const (
	fixtureSeed      = 3
	sanFranciscoSeed = 4
)

func TestEachTaskScoresItsPassingFixtureFullAndItsFailingOneLower(t *testing.T) {
	for _, recorded := range []struct {
		task, fixture string
		seed          int64
		failed        []int
	}{
		{"books", "books-pass", fixtureSeed, nil},
		{"books", "books-fail", fixtureSeed, []int{3, 5, 9}},
		{"herokuapp", "herokuapp-pass", fixtureSeed, nil},
		{"herokuapp", "herokuapp-fail", fixtureSeed, []int{2, 3, 5}},
		{"herokuapp", "herokuapp-delta", fixtureSeed, nil},
		{"herokuapp", "herokuapp-noafter", fixtureSeed, []int{3}},
		{"herokuapp", "herokuapp-star", fixtureSeed, nil},
		{"wikipedia", "wikipedia-pass", fixtureSeed, nil},
		{"wikipedia", "wikipedia-fail", fixtureSeed, []int{1, 4, 5}},
		{"wikipedia", "wikipedia-redirect", fixtureSeed, nil},
		{"wikipedia", "wikipedia-lead", fixtureSeed, nil},
		{"flights", "flights-pass", fixtureSeed, nil},
		{"flights", "flights-fail", fixtureSeed, []int{1, 4, 6}},
		{"flights", "flights-china", fixtureSeed, nil},
		{"flights", "flights-absent", fixtureSeed, []int{6}},
		{"flights", "flights-portuguese", sanFranciscoSeed, nil},
		{"flights", "flights-portuguese-wrongprice", sanFranciscoSeed, []int{5, 6}},
		{"youtube", "youtube-pass", fixtureSeed, nil},
		{"youtube", "youtube-fail", fixtureSeed, []int{2, 5}},
		{"youtube", "youtube-portuguese", fixtureSeed, nil},
		{"youtube", "youtube-portuguese-auto", fixtureSeed, []int{5}},
		{"youtube", "youtube-artifact", fixtureSeed, nil},
	} {
		task, err := Named(recorded.task, recorded.seed, drawnOn)
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
	t.Logf("seed 1: %s\nseed 2: %s\nseed %d: %s\nseed %d: %s", prompt(1), prompt(2), fixtureSeed, prompt(fixtureSeed), sanFranciscoSeed, prompt(sanFranciscoSeed))
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
