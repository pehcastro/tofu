package main

import (
	"strings"
	"testing"

	"tofu/interface/tui"
	"tofu/internal/llm"
)

func gateEvents(t *testing.T, events []tui.Event) (failure, gateOff string) {
	t.Helper()
	for _, event := range events {
		switch event.Kind {
		case tui.EventFailure:
			failure = event.Text
		case tui.EventGateOff:
			gateOff = event.Text
		}
	}
	return failure, gateOff
}

func TestARuleWithAThresholdOutOfRangeRefusesToStartTheTurn(t *testing.T) {
	dir := scratchProject(t)
	t.Setenv(envVarName(), fakeSecret("gate"))
	path := writeGateRuleWithThresholds(t, strings.Replace(gateFixtureThresholds, "relax_at: 0.85", "relax_at: 1.85", 1))

	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the turn should never reach a model"}}}
	var events eventLog
	liveAppSession(dir, model).run(t.Context(), wireSubscription, "write a note", events.add)

	failure, gateOff := gateEvents(t, events.all())
	if failure == "" {
		t.Fatalf("a rule with user_requested_relax_at 1.85 started a turn with no failure on screen: %+v", events.all())
	}
	if len(model.requests) != 0 {
		t.Fatalf("the turn asked the model %d times, and an unusable rule starts no turn", len(model.requests))
	}
	if gateOff != "" {
		t.Errorf("the unusable rule also said the gate is off, which is the missing-key line: %q", gateOff)
	}
	if !strings.Contains(failure, path) {
		t.Errorf("the refusal does not name the file %s:\n%s", path, failure)
	}
	if !strings.Contains(failure, "user_requested_relax_at") {
		t.Errorf("the refusal does not name the field that failed:\n%s", failure)
	}
	t.Logf("screen, failure: %s", failure)
}

func TestAMissingKeyStartsTheTurnWithTheGateOff(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote it"}}}
	var events eventLog
	liveAppSession(dir, model).run(t.Context(), wireSubscription, "write a note", events.add)

	failure, gateOff := gateEvents(t, events.all())
	if gateOff == "" {
		t.Fatalf("no key left no gate-off line on screen: %+v", events.all())
	}
	if failure != "" {
		t.Fatalf("a missing key refused the turn: %s", failure)
	}
	if len(model.requests) != 1 {
		t.Fatalf("the turn asked the model %d times, want once with the gate off", len(model.requests))
	}
	t.Logf("screen, gate off: %s", gateOff)
}
