package api

import (
	"testing"

	"tofu/internal/judge/question"
)

func TestGateBatteryResolvesAPinnedLibraryVersion(t *testing.T) {
	questions, version, err := GateBattery()
	if err != nil {
		t.Fatalf("GateBattery: %v, want it to resolve tool_gate@1 without ambiguity", err)
	}
	if version != 1 {
		t.Fatalf("GateBattery() named questions_version %d, want 1, the version bench-001 and every corpus fixture were measured against", version)
	}
	want := []string{"risk", "approval", "user_requested", "from_untrusted"}
	if len(questions) != len(want) {
		t.Fatalf("GateBattery() returned %d questions, want %d: %v", len(questions), len(want), want)
	}
	seen := make(map[string]bool, len(questions))
	for _, q := range questions {
		seen[q.ID] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("GateBattery() is missing the %q question", id)
		}
	}
}

func TestToJevBatteryCarriesAChoiceOptionsCriteriaThrough(t *testing.T) {
	questions := []question.Question{{
		Name: "pick",
		Kind: question.KindChoice,
		Options: []question.Option{
			{Name: "approve", Criteria: question.Criteria{Kind: question.CriteriaText, Text: "the state names an approved tool"}},
		},
	}}

	got := toJevBattery(questions)

	if len(got) != 1 || len(got[0].Options) != 1 {
		t.Fatalf("toJevBattery(%v) = %v, want one question with one option", questions, got)
	}
	criteria, ok := got[0].Options[0].Criteria.(string)
	if !ok || criteria != "the state names an approved tool" {
		t.Fatalf("toJevBattery() dropped the option's criteria, got %#v", got[0].Options[0].Criteria)
	}
}
