package main

import (
	"strings"
	"testing"

	"tofu/interface/tui"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

const pickedSonnet = "claude-sub/claude-sonnet-5"

func pickedRun(t *testing.T, pick tui.Pick) runOpts {
	t.Helper()
	return pickedOpts(t.TempDir(), "turn-1", "a task", pick, 0)
}

func TestAPickAndTheModelFlagChooseTheSameModel(t *testing.T) {
	flagged, err := parseRunArgs([]string{"--dir", t.TempDir(), "--wire", wireSubscription, "--model", pickedSonnet, "--effort", "high", "a task"})
	if err != nil {
		t.Fatal(err)
	}
	picked := pickedRun(t, tui.Pick{Wire: wireSubscription, Model: pickedSonnet, Effort: llm.EffortHigh})
	if picked.wire != flagged.wire || picked.model != flagged.model || picked.effort != flagged.effort {
		t.Fatalf("the picker builds wire %q model %q effort %q and --model builds wire %q model %q effort %q",
			picked.wire, picked.model, picked.effort, flagged.wire, flagged.model, flagged.effort)
	}
	fromFlag, flagErr := chooseModel(flagged)
	fromPick, pickErr := chooseModel(picked)
	if flagErr != nil || pickErr != nil {
		t.Fatalf("chooseModel refused one of the two: %v and %v", flagErr, pickErr)
	}
	if fromFlag.Slug() != pickedSonnet || fromPick.Slug() != fromFlag.Slug() {
		t.Fatalf("--model reached %s and the pick reached %s", fromFlag.Slug(), fromPick.Slug())
	}
}

func TestAPickWithNoModelRunsTheBoundTurnModel(t *testing.T) {
	bound, err := chooseModel(pickedRun(t, tui.Pick{Wire: wireSubscription}))
	if err != nil {
		t.Fatal(err)
	}
	if bound.Slug() == "" {
		t.Fatal("an empty pick reached no model at all")
	}
}

func TestAPickCannotPathAroundTheLibrary(t *testing.T) {
	excluded, err := chooseModel(pickedRun(t, tui.Pick{Wire: wireSubscription, Model: "claude-sub/claude-fable-5-1"}))
	if err == nil {
		t.Fatalf("an excluded model reached the turn as %s", excluded.Slug())
	}
	if !strings.Contains(err.Error(), "excludes claude-sub/claude-fable-5-1") {
		t.Fatalf("want the library exclusion named, got %v", err)
	}
	foreign, err := chooseModel(pickedRun(t, tui.Pick{Wire: wireCodex, Model: pickedSonnet}))
	if err == nil {
		t.Fatalf("a claude-sub model reached the codex wire as %s", foreign.Slug())
	}
	if !strings.Contains(err.Error(), "belongs to the claude-sub subscription") {
		t.Fatalf("want the subscription mismatch named, got %v", err)
	}
}

func TestEveryEffortThePickerOffersIsOneTheWireEncodes(t *testing.T) {
	ask := func(level llm.Effort) error {
		_, err := anthropic.Request{
			Model:    "claude-sonnet-5",
			Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
			Effort:   level,
		}.Encode(true)
		return err
	}
	for _, level := range wireEfforts(wireSubscription) {
		if err := ask(level); err != nil {
			t.Errorf("the picker offers %s on the anthropic wire and the wire refuses it: %v", level, err)
		}
	}
	if err := ask(llm.EffortMinimal); err == nil {
		t.Errorf("the anthropic wire encoded %s, so the shorter list the picker offers proves nothing", llm.EffortMinimal)
	}
	if wireEfforts(wireKey) != nil {
		t.Error("the openrouter wire sends no reasoning effort, so the picker must offer none")
	}
}
