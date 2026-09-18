package question

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func layerDir(t *testing.T, name string, body string) Layer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tool_gate@1.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return DirLayer(name, dir)
}

func fieldFile(t *testing.T, fields []Field, path string) string {
	t.Helper()
	for _, f := range fields {
		if f.Path == path {
			return f.File
		}
	}
	t.Fatalf("no field at %q in %v", path, fields)
	return ""
}

func overrideLayers(t *testing.T) []Layer {
	t.Helper()
	catalog := layerDir(t, "catalog", "name: tool_gate\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  risk:\n    type: score\n    instructions: from the catalog\n    criteria:\n      - low\n      - high\n")
	global := layerDir(t, "global", "questions:\n  risk:\n    instructions: from the global file\n  approval:\n    type: noul\n    instructions: from the global file\n")
	project := layerDir(t, "project", "questions:\n  risk:\n    instructions: from the project file\n")
	return []Layer{catalog, global, project}
}

func TestOverrideChainResolvesToTheProjectFile(t *testing.T) {
	layers := overrideLayers(t)
	set, fields, err := Resolve("tool_gate", layers)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	risk, ok := set.Question("risk")
	if !ok {
		t.Fatalf("no risk question in %v", set.Questions)
	}
	if risk.Instructions != "from the project file" {
		t.Errorf("risk instructions = %q, want the project file", risk.Instructions)
	}
	if got := fieldFile(t, fields, "questions.risk.instructions"); got != filepath.Join(layers[2].Origin, "tool_gate@1.yaml") {
		t.Errorf("risk instructions came from %q, want the project layer", got)
	}
}

func TestOverrideChainFallsThroughFieldByField(t *testing.T) {
	layers := overrideLayers(t)
	set, fields, err := Resolve("tool_gate", layers)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	risk, _ := set.Question("risk")
	if len(risk.Levels) != 2 || risk.Levels[0].Text != "low" || risk.Levels[1].Text != "high" {
		t.Errorf("risk levels = %v, want the two the catalog declares", risk.Levels)
	}
	if risk.Kind != KindScore {
		t.Errorf("risk kind = %q, want the catalog type", risk.Kind)
	}
	approval, ok := set.Question("approval")
	if !ok {
		t.Fatalf("the global question was dropped, questions are %v", set.Questions)
	}
	if approval.Instructions != "from the global file" {
		t.Errorf("approval instructions = %q", approval.Instructions)
	}
	if set.QuestionsVersion != 1 || len(set.State) != 1 {
		t.Errorf("questions_version = %d, state = %v, want the catalog values", set.QuestionsVersion, set.State)
	}
	if got := fieldFile(t, fields, "questions.risk.criteria[0]"); got != filepath.Join(layers[0].Origin, "tool_gate@1.yaml") {
		t.Errorf("risk criteria came from %q, want the catalog layer", got)
	}
	if got := fieldFile(t, fields, "questions.approval.instructions"); got != filepath.Join(layers[1].Origin, "tool_gate@1.yaml") {
		t.Errorf("approval instructions came from %q, want the global layer", got)
	}
}

func TestResolvePrintsEachFieldAndItsFile(t *testing.T) {
	layers := overrideLayers(t)
	_, fields, err := Resolve("tool_gate", layers)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("no fields resolved")
	}
	for _, f := range fields {
		t.Logf("%-40s %-24s %s:%d", f.Path, f.Value, f.File, f.Line)
	}
}

func TestResolveRefusesAnAbsentSet(t *testing.T) {
	if _, _, err := Resolve("no_such_set", overrideLayers(t)); err == nil {
		t.Fatal("resolve accepted a set no layer carries")
	}
}

func twoVersionDiskLayer(t *testing.T) Layer {
	t.Helper()
	dir := t.TempDir()
	body1 := "name: tool_gate\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  risk:\n    type: score\n    instructions: v1\n    criteria:\n      - low\n      - high\n"
	body2 := "name: tool_gate\nquestions_version: 2\nstate:\n  - tool\nquestions:\n  risk:\n    type: score\n    instructions: v2\n    criteria:\n      - low\n      - high\n"
	if err := os.WriteFile(filepath.Join(dir, "tool_gate@1.yaml"), []byte(body1), 0o644); err != nil {
		t.Fatalf("write @1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool_gate@2.yaml"), []byte(body2), 0o644); err != nil {
		t.Fatalf("write @2: %v", err)
	}
	return DirLayer("catalog", dir)
}

func TestBareNameWithTwoVersionsOnADiskLayerIsRefused(t *testing.T) {
	layer := twoVersionDiskLayer(t)
	_, _, err := Resolve("tool_gate", []Layer{layer})
	if err == nil {
		t.Fatal("resolve accepted a bare name with two versions on disk")
	}
	for _, want := range []string{"tool_gate@1.yaml", "tool_gate@2.yaml", "tool_gate@1", "tool_gate@2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestBareNameWithOneVersionStillResolves(t *testing.T) {
	layer := layerDir(t, "catalog", "name: tool_gate\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  risk:\n    type: score\n    instructions: v1\n    criteria:\n      - low\n      - high\n")
	set, _, err := Resolve("tool_gate", []Layer{layer})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if set.Version != 1 {
		t.Errorf("version = %d, want 1", set.Version)
	}
}

func TestExactVersionResolvesDespiteTwoVersionsOnDisk(t *testing.T) {
	layer := twoVersionDiskLayer(t)
	set, _, err := Resolve("tool_gate@1", []Layer{layer})
	if err != nil {
		t.Fatalf("resolve tool_gate@1: %v", err)
	}
	if set.Version != 1 {
		t.Errorf("version = %d, want 1", set.Version)
	}
	risk, _ := set.Question("risk")
	if risk.Instructions != "v1" {
		t.Errorf("risk instructions = %q, want v1", risk.Instructions)
	}
}
