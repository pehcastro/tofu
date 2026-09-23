package thrift

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadRuleReadsModeAndKeepAt(t *testing.T) {
	fsys := fstest.MapFS{
		"thrift_rule@1.yaml": &fstest.MapFile{Data: []byte("name: thrift_rule\nmode: enforced\nkeep_at: 0.42\n")},
	}
	rule, err := LoadRule(fsys, "thrift_rule@1")
	if err != nil {
		t.Fatalf("LoadRule: %v", err)
	}
	if rule.Mode != ModeEnforced || rule.KeepAt != 0.42 {
		t.Fatalf("got mode %s keep_at %g, want enforced 0.42", rule.Mode, rule.KeepAt)
	}
}

func TestLoadRuleRefusesAnUnknownMode(t *testing.T) {
	fsys := fstest.MapFS{"thrift_rule@1.yaml": &fstest.MapFile{Data: []byte("mode: shadow\nkeep_at: 0.5\n")}}
	_, err := LoadRule(fsys, "thrift_rule@1")
	if err == nil || !strings.Contains(err.Error(), "mode is off or enforced") {
		t.Fatalf("an unknown mode was accepted: %v", err)
	}
}

func TestLoadRuleRefusesAMissingKeepAt(t *testing.T) {
	fsys := fstest.MapFS{"thrift_rule@1.yaml": &fstest.MapFile{Data: []byte("mode: off\n")}}
	_, err := LoadRule(fsys, "thrift_rule@1")
	if err == nil || !strings.Contains(err.Error(), "names no keep_at") {
		t.Fatalf("a missing keep_at was accepted: %v", err)
	}
}

func TestDecideKeepsAtOrAboveTheThreshold(t *testing.T) {
	mark, err := Decide(0, 0.5, true, 0.5)
	if err != nil || !mark.Keep {
		t.Fatalf("Decide(0.5, keep_at 0.5) = %+v, %v, want kept", mark, err)
	}
	mark, err = Decide(0, 0.49, true, 0.5)
	if err != nil || mark.Keep {
		t.Fatalf("Decide(0.49, keep_at 0.5) = %+v, %v, want dropped", mark, err)
	}
}

func TestDecideRefusesAnUnansweredUnit(t *testing.T) {
	_, err := Decide(3, 0, false, 0.5)
	if err == nil || !strings.Contains(err.Error(), "unit 3") {
		t.Fatalf("an unanswered unit was decided anyway: %v", err)
	}
}
