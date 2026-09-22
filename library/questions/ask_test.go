package questions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/question"
	"tofu/library/questions"
)

const askPoint = "ask@1"

func askLayers(t *testing.T, extra ...question.Layer) []question.Layer {
	t.Helper()
	return append([]question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}}, extra...)
}

func TestShippedAskAsksAChoiceAndANoulAndPassesItsOwnLint(t *testing.T) {
	set, _, err := question.Resolve(askPoint, askLayers(t))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(set.Questions) != 2 || set.Questions[0].Name != "action" || set.Questions[1].Name != "determined" {
		t.Fatalf("the set asks %v, want action then determined", set.Questions)
	}
	if set.Questions[0].Kind != question.KindChoice {
		t.Fatalf("action is a %s, want a choice", set.Questions[0].Kind)
	}
	if set.Questions[1].Kind != question.KindNoul {
		t.Fatalf("determined is a %s, want a noul", set.Questions[1].Kind)
	}
	if findings := question.Lint(set, question.DefaultCaps()); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("lint: %s", f)
		}
	}
	names := make([]string, len(set.Questions[0].Options))
	for i, o := range set.Questions[0].Options {
		names[i] = o.Name
	}
	for _, want := range []string{"proceed", "ask_now", "defer_to_end"} {
		if !strings.Contains(strings.Join(names, ","), want) {
			t.Errorf("action does not offer %q, has %v", want, names)
		}
	}
	if set.Questions[0].Escape != "ask_now" {
		t.Errorf("action's escape is %q, want ask_now", set.Questions[0].Escape)
	}
}

func TestAProjectLibraryOverridesTheShippedAskWording(t *testing.T) {
	dir := t.TempDir()
	override := "name: ask\nquestions_version: 1\nquestions:\n  determined:\n    type: noul\n    instructions: >-\n      the project asks it differently, naming `precedent_exists` itself\n"
	if err := os.WriteFile(filepath.Join(dir, "ask@1.yaml"), []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}

	shipped, _, err := question.Resolve(askPoint, askLayers(t))
	if err != nil {
		t.Fatalf("resolve the shipped set: %v", err)
	}
	merged, _, err := question.Resolve(askPoint, askLayers(t, question.DirLayer("project", dir)))
	if err != nil {
		t.Fatalf("resolve with a project layer: %v", err)
	}
	shippedDetermined, _ := shipped.Question("determined")
	mergedDetermined, _ := merged.Question("determined")
	if mergedDetermined.Instructions == shippedDetermined.Instructions {
		t.Fatal("the project layer did not change the wording")
	}
	if !strings.Contains(mergedDetermined.Instructions, "the project asks it differently") {
		t.Fatalf("the project wording did not win: %s", mergedDetermined.Instructions)
	}
	mergedAction, _ := merged.Question("action")
	shippedAction, _ := shipped.Question("action")
	if mergedAction.Instructions != shippedAction.Instructions {
		t.Fatal("overriding determined changed the untouched action question")
	}
}
