package questions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/question"
	"tofu/internal/sys"
	"tofu/library/questions"
)

const shellSiftPoint = "shell_sift@1"

func shellSiftLayers(t *testing.T, extra ...question.Layer) []question.Layer {
	t.Helper()
	return append([]question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}}, extra...)
}

func TestShippedShellSiftAsksOneNoulAndPassesItsOwnLint(t *testing.T) {
	set, _, err := question.Resolve(shellSiftPoint, shellSiftLayers(t))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(set.Questions) != 1 || set.Questions[0].Name != "still_needed" {
		t.Fatalf("the set asks %d questions, want one named still_needed", len(set.Questions))
	}
	if set.Questions[0].Kind != question.KindNoul {
		t.Fatalf("still_needed is a %s, want a noul", set.Questions[0].Kind)
	}
	if findings := question.Lint(set, question.DefaultCaps()); len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("lint: %s", f)
		}
	}
	for _, want := range []string{"still needs this chunk", "not against how interesting it looks"} {
		if !strings.Contains(set.Questions[0].Instructions, want) {
			t.Errorf("the shipped wording does not carry %q: %s", want, set.Questions[0].Instructions)
		}
	}
}

func TestAProjectLibraryOverridesTheShippedShellSiftWording(t *testing.T) {
	dir := t.TempDir()
	override := "name: shell_sift\nquestions_version: 1\nquestions:\n  still_needed:\n    type: noul\n    instructions: >-\n      the project asks it differently, naming `chunk` and `task` itself\n"
	if err := os.WriteFile(filepath.Join(dir, "shell_sift@1.yaml"), []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}

	shipped, _, err := question.Resolve(shellSiftPoint, shellSiftLayers(t))
	if err != nil {
		t.Fatalf("resolve the shipped set: %v", err)
	}
	merged, _, err := question.Resolve(shellSiftPoint, shellSiftLayers(t, sys.DirLayer("project", dir)))
	if err != nil {
		t.Fatalf("resolve with a project layer: %v", err)
	}
	if merged.Questions[0].Instructions == shipped.Questions[0].Instructions {
		t.Fatal("the project layer did not change the wording")
	}
	if !strings.Contains(merged.Questions[0].Instructions, "the project asks it differently") {
		t.Fatalf("the project wording did not win: %s", merged.Questions[0].Instructions)
	}
	if merged.Questions[0].True.Text != shipped.Questions[0].True.Text {
		t.Fatal("overriding the instructions dropped the shipped true criteria")
	}
}
