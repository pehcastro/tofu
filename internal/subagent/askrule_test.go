package subagent

import (
	"strings"
	"testing"

	"tofu/internal/judge/gate"
	shipped "tofu/library"
)

func TestTheShippedAskRuleDeclaresItsOwnSchemaAndIsShadow(t *testing.T) {
	r, err := LoadAskRule(shipped.Files(), "ask@1")
	if err != nil {
		t.Fatalf("load the shipped rule: %v", err)
	}
	if r.Schema != AskSchema {
		t.Fatalf("library/general/rules/ask@1.yaml declares schema %q, want %q", r.Schema, AskSchema)
	}
	if r.Mode != gate.ModeShadow {
		t.Fatalf("library/general/rules/ask@1.yaml is %s, want shadow", r.Mode)
	}
	if r.DeterminedLowAt <= 0 || r.DeterminedLowAt > 1 {
		t.Fatalf("determined_low_at is %g, want a noul threshold in (0, 1]", r.DeterminedLowAt)
	}
}

func TestLoadAskRuleRefusesTheWrongSchema(t *testing.T) {
	_, err := LoadAskRule(shipped.Files(), "shell_sift@1")
	if err == nil || !strings.Contains(err.Error(), "shell_sift") {
		t.Fatalf("got %v, want a refusal naming the mismatched schema", err)
	}
}
