package gate

import (
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/question"
	shipped "tofu/library"
)

func TestARuleDeclaringNoSchemaIsAGateRuleAndKeepsTodaysRefusal(t *testing.T) {
	r, err := LoadFS(shipped.Files(), "tool_gate@6")
	if err != nil {
		t.Fatalf("load the shipped gate rule: %v", err)
	}
	if r.Schema != SchemaGate {
		t.Fatalf("tool_gate@6 declares schema %q, want %q by default", r.Schema, SchemaGate)
	}

	path := filepath.Join(t.TempDir(), "bad@1.yaml")
	writeFile(t, path, "name: bad\ndomain: general\nkind: threshold\nschema: gate\nnot_a_field: 1\n")
	_, err = Load(path)
	if err == nil {
		t.Fatal("a rule declaring schema gate accepted an unknown field")
	}
	if !strings.Contains(err.Error(), `unknown field "not_a_field"`) {
		t.Fatalf("the refusal does not name the field: %v", err)
	}
}

func TestAForeignSchemaKeepsTheSharedFieldsAndIgnoresTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell_sift@1.yaml")
	writeFile(t, path, "name: shell_sift\ndomain: shell\nkind: threshold\nschema: shell_sift\nrule_version: 1\nquestions: shell_sift\nquestions_version: 1\nmode: shadow\nsample_floor: 300\nnotes: a note\n\nthresholds:\n  keep_at: 0.5\n")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Schema != "shell_sift" || r.Name != "shell_sift" || r.RuleVersion != 1 {
		t.Fatalf("the shared fields did not survive: %+v", r)
	}
	if r.Questions != "shell_sift" || r.QuestionsVersion != 1 || r.Mode != ModeShadow || !r.ModeDeclared || r.SampleFloor != 300 {
		t.Fatalf("the shared fields did not survive: %+v", r)
	}
	if r.Thresholds != (Thresholds{}) {
		t.Fatalf("a foreign schema carried gate thresholds: %+v", r.Thresholds)
	}
}

func TestAForeignSchemaIsNotLintedAgainstTheGatesFourRoles(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "library", "questions", "shell_sift@1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := question.Load(path)
	if err != nil {
		t.Fatalf("question.Load: %v", err)
	}
	t.Chdir(t.TempDir())
	r, origin, err := LoadPoint(shipped.Files(), "shell_sift@1", set)
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if r.Schema != "shell_sift" || origin != OriginBinary {
		t.Fatalf("LoadPoint read %+v from %s", r, origin)
	}
	if findings := Lint(r, set); len(findings) != 4 {
		t.Fatalf("the gate lint found %d findings on a one-question point, and it is the reason LoadPoint skips it", len(findings))
	}
}

func TestAForeignSchemaCarriesItsThresholdNumbers(t *testing.T) {
	r, err := LoadFS(shipped.Files(), "shell_sift@1")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if r.ForeignThresholds["keep_at"] != 0.5 {
		t.Fatalf("keep_at is %g, want 0.5", r.ForeignThresholds["keep_at"])
	}
}

func TestAForeignThresholdThatIsNotANumberFailsTheLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell_sift@1.yaml")
	writeFile(t, path, "name: shell_sift\ndomain: shell\nkind: threshold\nschema: shell_sift\nthresholds:\n  keep_at: banana\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("a non-number threshold in a foreign schema was accepted")
	}
	if !strings.Contains(err.Error(), path+":6:") || !strings.Contains(err.Error(), "keep_at") {
		t.Fatalf("the refusal does not name the file and the line: %v", err)
	}
}

func TestASchemaNameThatIsNotOneIsRefused(t *testing.T) {
	for _, bad := range []string{"Gate", "shell sift", "shell-sift", "1sift", ""} {
		path := filepath.Join(t.TempDir(), "bad@1.yaml")
		writeFile(t, path, "name: bad\ndomain: general\nkind: threshold\nschema: "+bad+"\n")
		if _, err := Load(path); err == nil {
			t.Errorf("schema %q was accepted", bad)
		}
	}
}
