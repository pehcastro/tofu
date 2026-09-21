package testquality

import (
	"strings"
	"testing"
)

func TestPromptAppendsRuleGuidanceOnlyForTheOnArm(t *testing.T) {
	description := "write a test for Add"
	off := Prompt(description, ArmRulesOff)
	if off != description {
		t.Fatalf("off arm = %q, want the description unchanged", off)
	}
	on := Prompt(description, ArmRulesOn)
	if !strings.HasPrefix(on, description) {
		t.Fatalf("on arm does not start with the description: %q", on)
	}
	if !strings.Contains(on, "No assertion that cannot fail") {
		t.Fatalf("on arm does not carry the rule guidance: %q", on)
	}
}
