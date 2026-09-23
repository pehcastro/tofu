package turn

import (
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/rule"
)

func spec(rules ...rule.Rule) ComposeSpec {
	return ComposeSpec{
		Task:         "rename one symbol",
		Environment:  "<env>\nworking directory: /w\n</env>",
		ToolGuidance: "read reads a whole file",
		Rules:        rules,
	}
}

func firingRules() []rule.Rule {
	return []rule.Rule{
		{ID: "no_worktree", Concern: rule.ConcernSafety, Notes: "never git worktree", File: "library/dev/rules/no_worktree@1.yaml"},
		{ID: "em_dash", Concern: rule.ConcernOutputShape, Notes: "never a literal U+2014", File: "library/general/rules/em_dash@1.yaml"},
	}
}

func TestARuleThatFiresAndCarriesNoTextFailsTheComposition(t *testing.T) {
	hollow := rule.Rule{ID: "hollow", Concern: rule.ConcernCodeRules, File: "library/dev/rules/hollow@1.yaml"}
	_, err := Compose(spec(append(firingRules(), hollow)...))
	if err == nil {
		t.Fatal("a rule that fires and carries no text was dropped from the prompt in silence")
	}
	for _, wanted := range []string{"hollow", "always on", "library/dev/rules/hollow@1.yaml"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Fatalf("the failure does not name %q: %v", wanted, err)
		}
	}
}

func TestADroppedAlwaysOnConcernFailsTheComposition(t *testing.T) {
	for _, one := range []struct {
		name    string
		spec    ComposeSpec
		concern rule.Concern
	}{
		{name: "no environment", spec: ComposeSpec{Task: "t", ToolGuidance: "g", Rules: firingRules()}, concern: rule.ConcernEnvironment},
		{name: "no tool guidance", spec: ComposeSpec{Task: "t", Environment: "e", Rules: firingRules()}, concern: rule.ConcernToolGuidance},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := Compose(one.spec)
			if err == nil {
				t.Fatalf("composing dropped %s and said nothing", one.concern)
			}
			if !strings.Contains(err.Error(), string(one.concern)) {
				t.Fatalf("the failure does not name %s: %v", one.concern, err)
			}
		})
	}
}

func TestARuleWithTextSendsItsTextRatherThanItsNotes(t *testing.T) {
	human := rule.Rule{
		ID:      "quote",
		Concern: rule.ConcernTaskShaping,
		Text:    "cite the turn rather than paraphrase it",
		Notes:   "moved here by another ticket",
		File:    "library/general/rules/quote@1.yaml",
	}
	composed, err := Compose(spec(append(firingRules(), human)...))
	if err != nil {
		t.Fatal(err)
	}
	system := composed.System()
	if !strings.Contains(system, human.Text) {
		t.Fatalf("the composed prompt does not carry the text of the rule %s:\n%s", human.ID, system)
	}
	if strings.Contains(system, human.Notes) {
		t.Fatalf("the composed prompt sent what a maintainer reads rather than what the model reads:\n%s", system)
	}
}

func TestTheComposedSystemPromptNamesTheRuleEachPartCameFrom(t *testing.T) {
	composed, err := Compose(spec(firingRules()...))
	if err != nil {
		t.Fatal(err)
	}
	system := composed.System()
	for _, wanted := range []string{
		"[safety, from the rule no_worktree]",
		"never git worktree",
		"[output_shape, from the rule em_dash]",
		"[tool_guidance, from tofu itself]",
		"[format_contract, from tofu itself]",
	} {
		if !strings.Contains(system, wanted) {
			t.Fatalf("the composed system prompt does not carry %q:\n%s", wanted, system)
		}
	}
	if strings.Contains(system, "working directory: /w") {
		t.Fatalf("the environment reached the cached system prompt, where a per turn value does not belong:\n%s", system)
	}
}

func TestTheComposedSystemPromptOverTheRealLibraryCarriesNoHostPath(t *testing.T) {
	library, err := filepath.Abs(filepath.Join("..", "..", "library"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := rule.LoadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	composed, err := Compose(ComposeSpec{
		Task:         "write a test in internal/turn/compose.go",
		Environment:  "<env>\nworking directory: /w\n</env>",
		ToolGuidance: "read reads a whole file",
		Rules:        rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	system := composed.System()
	root := filepath.ToSlash(filepath.Dir(library))
	for _, spelling := range []string{root, filepath.Dir(library)} {
		if strings.Contains(system, spelling) {
			t.Fatalf("the composed system prompt sends the host path %s to a provider:\n%s", spelling, system)
		}
	}
	fired := 0
	for _, part := range composed.Parts {
		if part.RuleID == "" {
			continue
		}
		fired++
		if !strings.Contains(system, "from the rule "+part.RuleID+"]") {
			t.Fatalf("the composed system prompt does not name the rule %s:\n%s", part.RuleID, system)
		}
		if !strings.Contains(part.From(), part.File) {
			t.Fatalf("--show-prompt lost the file of the rule %s, which reads %s", part.RuleID, part.From())
		}
	}
	if fired == 0 {
		t.Fatalf("no rule in the real library at %s fired, so the prompt was never at risk of carrying a path", library)
	}
}

func TestTheTaskNamesItsPathsAndItsVerb(t *testing.T) {
	task := TaskNamed("debug the failing test in internal/turn/loop_test.go and in README.md, not a.b")
	if want := []string{"internal/turn/loop_test.go", "README.md"}; len(task.Paths) != len(want) {
		t.Fatalf("expected the paths %v, got %v", want, task.Paths)
	}
	for i, want := range []string{"internal/turn/loop_test.go", "README.md"} {
		if task.Paths[i] != want {
			t.Fatalf("expected path %d to be %s, got %s", i, want, task.Paths[i])
		}
	}
	if task.Verb != rule.VerbDebug {
		t.Fatalf("expected the verb %s, got %q", rule.VerbDebug, task.Verb)
	}
	if plain := TaskNamed("rename one symbol"); len(plain.Paths) != 0 || plain.Verb != rule.VerbNone {
		t.Fatalf("a task naming no path and no verb read as paths %v and verb %q", plain.Paths, plain.Verb)
	}
}
