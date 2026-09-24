package gate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"tofu/internal/judge/question"
	shipped "tofu/library"
)

func toolGateRulePath() string {
	return filepath.Join("..", "..", "..", "library", "general", "rules", "tool_gate@1.yaml")
}

const projectToolGateRule = `name: tool_gate
domain: general
kind: threshold
rule_version: 1
questions: tool_gate
questions_version: 1
mode: shadow
risk_question: risk
approval_question: approval
user_requested_question: user_requested
from_untrusted_question: from_untrusted

thresholds:
  risk_ask_at: 0.25
`

func shippedQuestionSet(t *testing.T) question.Set {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "library", "questions", "tool_gate@1.yaml"))
	if err != nil {
		t.Fatalf("absolute question path: %v", err)
	}
	set, err := question.Load(path)
	if err != nil {
		t.Fatalf("question.Load: %v", err)
	}
	return set
}

func projectRuleDir(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "library", "general", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}
	return root, filepath.Join(dir, "tool_gate@1.yaml")
}

func TestLoadPointReadsTheEmbeddedRuleWhenTheProjectHasNoLibrary(t *testing.T) {
	set := shippedQuestionSet(t)
	t.Chdir(t.TempDir())
	r, origin, err := LoadPoint(shipped.Files(), "tool_gate@1", set, "")
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if origin != OriginBinary {
		t.Fatalf("origin = %q, want %q", origin, OriginBinary)
	}
	if r.Name != "tool_gate" || r.Thresholds.RiskAskAt != 1.5 {
		t.Fatalf("name = %q risk_ask_at = %v, want tool_gate and 1.5", r.Name, r.Thresholds.RiskAskAt)
	}
	if r.File != "library/general/rules/tool_gate@1.yaml" {
		t.Fatalf("file = %q, want the embedded name", r.File)
	}
}

func TestLoadPointPrefersTheProjectRuleOverTheEmbeddedOne(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectRuleDir(t)
	writeFile(t, path, projectToolGateRule)
	t.Chdir(root)
	r, origin, err := LoadPoint(shipped.Files(), "tool_gate@1", set, "")
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if origin != OriginProject {
		t.Fatalf("origin = %q, want %q", origin, OriginProject)
	}
	if r.Thresholds.RiskAskAt != 0.25 {
		t.Fatalf("risk_ask_at = %v, want 0.25 from the project rule", r.Thresholds.RiskAskAt)
	}
	if r.File != path {
		t.Fatalf("file = %q, want %q", r.File, path)
	}
}

func TestLoadPointReadsTheNamedDirectoryRatherThanTheWorkingOne(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectRuleDir(t)
	writeFile(t, path, projectToolGateRule)
	t.Chdir(t.TempDir())
	r, origin, err := LoadPoint(shipped.Files(), "tool_gate@1", set, root)
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if origin != OriginProject || r.Thresholds.RiskAskAt != 0.25 {
		t.Fatalf("origin = %q risk_ask_at = %v, want the project rule in %s", origin, r.Thresholds.RiskAskAt, root)
	}
	if r.File != path {
		t.Fatalf("file = %q, want %q", r.File, path)
	}
}

func TestLoadPointLeavesTheWorkingDirectorysRuleAloneWhenAnotherIsNamed(t *testing.T) {
	set := shippedQuestionSet(t)
	standing, path := projectRuleDir(t)
	writeFile(t, path, projectToolGateRule)
	t.Chdir(standing)
	r, origin, err := LoadPoint(shipped.Files(), "tool_gate@1", set, t.TempDir())
	if err != nil {
		t.Fatalf("LoadPoint: %v", err)
	}
	if origin != OriginBinary || r.Thresholds.RiskAskAt != 1.5 {
		t.Fatalf("origin = %q risk_ask_at = %v, want the binary rule; the working directory decided instead", origin, r.Thresholds.RiskAskAt)
	}
}

func TestLoadPointFailsWhenTheProjectRuleIsUnusable(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectRuleDir(t)
	writeFile(t, path, "name: tool_gate\ndomain: general\nkind: threshold\n\tindented_with_a_tab: 1\n")
	t.Chdir(root)
	r, origin, err := LoadPoint(shipped.Files(), "tool_gate@1", set, "")
	if err == nil {
		t.Fatalf("LoadPoint fell back to %s with an unusable project rule: %+v", origin, r)
	}
	if !strings.Contains(err.Error(), "the project") {
		t.Fatalf("the error does not say the project rule failed: %v", err)
	}
}

func TestLoadPointFailsWhenTheProjectRuleDoesNotMatchTheQuestions(t *testing.T) {
	set := shippedQuestionSet(t)
	root, path := projectRuleDir(t)
	writeFile(t, path, strings.Replace(projectToolGateRule, "risk_question: risk", "risk_question: danger", 1))
	t.Chdir(root)
	if _, _, err := LoadPoint(shipped.Files(), "tool_gate@1", set, ""); err == nil {
		t.Fatal("LoadPoint accepted a project rule naming a question the set does not have")
	}
}

func TestLoadFSNamesTheDirectoryAndTheConventionWhenARuleFailsToParse(t *testing.T) {
	shipped := fstest.MapFS{
		"tools/shell/rules/broken@1.yaml": {Data: []byte("domain: general\nkind: threshold\nrule_version: 1\n")},
	}
	_, err := LoadFS(shipped, "broken@1")
	if err == nil {
		t.Fatal("LoadFS accepted a rule that declares no name")
	}
	if !strings.Contains(err.Error(), "library/tools/shell/rules") {
		t.Fatalf("the error does not name the directory the file was read from: %v", err)
	}
	if !strings.Contains(err.Error(), `was read as a rule because its directory is named "rules"`) {
		t.Fatalf("the error does not say the directory's name is why the file was read: %v", err)
	}
}

func thresholdFilesOnDisk(t *testing.T, library string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(library, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Base(filepath.Dir(name)) != "rules" || !strings.HasSuffix(name, ".yaml") {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "kind: "+ThresholdKind) {
			found = append(found, strings.TrimSuffix(filepath.Base(name), ".yaml"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", library, err)
	}
	return found
}

func TestRefsRecruitsExactlyTheThresholdFilesOnDisk(t *testing.T) {
	onDisk := thresholdFilesOnDisk(t, filepath.Join("..", "..", "..", "library"))
	refs, err := Refs(shipped.Files())
	if err != nil {
		t.Fatalf("Refs: %v", err)
	}
	if len(refs) != len(onDisk) {
		t.Fatalf("Refs = %d, want the %d threshold files on disk: refs=%v onDisk=%v", len(refs), len(onDisk), refs, onDisk)
	}
}

func TestLoadTheShippedRule(t *testing.T) {
	r, err := Load(toolGateRulePath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Name != "tool_gate" || r.RuleVersion != 1 {
		t.Fatalf("name = %q version = %d", r.Name, r.RuleVersion)
	}
	if r.Questions != "tool_gate" || r.QuestionsVersion != 1 {
		t.Fatalf("questions = %q questions_version = %d", r.Questions, r.QuestionsVersion)
	}
	if r.Thresholds.RiskAskAt != 1.5 || r.Thresholds.RiskDenyAt != 2.5 {
		t.Fatalf("thresholds = %+v", r.Thresholds)
	}
	if r.Thresholds.UserRequestedRelaxAt != 0.85 {
		t.Fatalf("user_requested_relax_at = %v", r.Thresholds.UserRequestedRelaxAt)
	}
}

func TestLoadRefusesARuleWithNoDomainByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool_gate@1.yaml")
	writeFile(t, path, "name: tool_gate\nkind: threshold\nrule_version: 1\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted a rule that declares no domain")
	}
	if !strings.HasPrefix(err.Error(), path) || !strings.Contains(err.Error(), "no domain") {
		t.Fatalf("the refusal does not name the file and the field: %v", err)
	}
}

func TestLoadRejectsAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad@1.yaml")
	writeFile(t, path, "name: bad\nnot_a_field: 1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown field")
	}
}

func TestAParseFailureNamesTheLineItHappenedOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad@1.yaml")
	writeFile(t, path, "name: bad\nrule_version: 1\n\nnot_a_field: 1\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted an unknown field")
	}
	want := path + `:4: unknown field "not_a_field"`
	if err.Error() != want {
		t.Fatalf("Load reported %q, want %q", err, want)
	}
}

func TestLoadDefaultsToShadowWhenModeIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noMode@1.yaml")
	writeFile(t, path, "name: no_mode\ndomain: general\nkind: threshold\nrule_version: 1\n")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", r.Mode)
	}
	if r.ModeDeclared {
		t.Fatalf("ModeDeclared = true, want false, mode was never written")
	}
}

func TestLoadRejectsAnUnknownMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badMode@1.yaml")
	writeFile(t, path, "name: bad_mode\ndomain: general\nkind: threshold\nrule_version: 1\nmode: sometimes\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown mode value")
	}
}

func TestLoadReadsSampleFloorAndDeclaredMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "declared@1.yaml")
	writeFile(t, path, "name: declared\ndomain: general\nkind: threshold\nrule_version: 1\nmode: enforced\nsample_floor: 300\n")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Mode != ModeEnforced || !r.ModeDeclared {
		t.Fatalf("mode = %s declared = %v", r.Mode, r.ModeDeclared)
	}
	if r.SampleFloor != 300 {
		t.Fatalf("sample_floor = %d, want 300", r.SampleFloor)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
