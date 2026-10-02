package tasks

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"tofu/bench/browser/airbnb"
)

var (
	drawnOn     = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	val7DrawnOn = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	drawnApart  = map[string]time.Time{"flights-seed7": val7DrawnOn, "flights-seed7-wrongstops": val7DrawnOn}
)

const (
	fixtureSeed      = 3
	sanFranciscoSeed = 4
	cityOfGodSeed    = 6
	madridSeed       = 7
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
		{"herokuapp", "herokuapp-batched", fixtureSeed, nil},
		{"herokuapp", "herokuapp-batched-old", fixtureSeed, []int{1, 2, 3}},
		{"herokuapp", "herokuapp-batched-reset", fixtureSeed, []int{2, 3}},
		{"wikipedia", "wikipedia-pass", fixtureSeed, nil},
		{"wikipedia", "wikipedia-fail", fixtureSeed, []int{1, 4, 5}},
		{"wikipedia", "wikipedia-redirect", fixtureSeed, nil},
		{"wikipedia", "wikipedia-lead", fixtureSeed, nil},
		{"flights", "flights-pass", fixtureSeed, nil},
		{"flights", "flights-fail", fixtureSeed, []int{1, 4, 6}},
		{"flights", "flights-china", fixtureSeed, nil},
		{"flights", "flights-absent", fixtureSeed, []int{6}},
		{"flights", "flights-window", fixtureSeed, nil},
		{"flights", "flights-portuguese", sanFranciscoSeed, nil},
		{"flights", "flights-portuguese-wrongprice", sanFranciscoSeed, []int{5, 6}},
		{"flights", "flights-seed7", madridSeed, nil},
		{"flights", "flights-seed7-wrongstops", madridSeed, []int{7}},
		{"youtube", "youtube-pass", fixtureSeed, nil},
		{"youtube", "youtube-fail", fixtureSeed, []int{2, 5}},
		{"youtube", "youtube-portuguese", fixtureSeed, nil},
		{"youtube", "youtube-portuguese-auto", fixtureSeed, []int{5}},
		{"youtube", "youtube-artifact", fixtureSeed, nil},
		{"npm", "npm-pass", fixtureSeed, nil},
		{"npm", "npm-fail", fixtureSeed, []int{1, 2, 5}},
		{"npm", "npm-searchlate", fixtureSeed, nil},
		{"imdb", "imdb-pass", fixtureSeed, nil},
		{"imdb", "imdb-fail", fixtureSeed, []int{3, 6}},
		{"imdb", "imdb-portuguese", fixtureSeed, nil},
		{"imdb", "imdb-recorded", cityOfGodSeed, nil},
		{"imdb", "imdb-recorded-wrongrating", cityOfGodSeed, []int{6}},
	} {
		task, err := Named(recorded.task, recorded.seed, cmp.Or(drawnApart[recorded.fixture], drawnOn))
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
	first, again := prompt(1), prompt(1)
	if first != again || first == prompt(2) {
		t.Error("seed 1 did not draw one flight, or seeds 1 and 2 drew the same one")
	}
	youtube, err := Named("youtube", fixtureSeed, drawnOn)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("youtube seed %d: %s", fixtureSeed, youtube.Prompt)
}

func TestSeedsSixAndSevenDrawAnotherPackageAndAnotherFilm(t *testing.T) {
	for _, name := range []string{"npm", "imdb"} {
		var prompts []string
		for _, seed := range []int64{6, 7} {
			task, err := Named(name, seed, drawnOn)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s seed %d: %s", name, seed, task.Prompt)
			prompts = append(prompts, task.Prompt)
		}
		if prompts[0] == prompts[1] {
			t.Errorf("%s: seeds 6 and 7 drew the same prompt", name)
		}
	}
}

func TestAnUnknownTaskIsRefused(t *testing.T) {
	if _, err := Named("airbnb-by-another-name", 0, drawnOn); err == nil {
		t.Error("an unknown task name was accepted")
	}
}
