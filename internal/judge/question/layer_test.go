package question

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"tofu/internal/sys"
)

func setBody(from string) []byte {
	return []byte("name: tool_gate\ndomain: general\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  risk:\n    type: score\n    instructions: from " + from + "\n    criteria:\n      - low\n      - high\n")
}

func shippedSet() fstest.MapFS {
	return fstest.MapFS{"tool_gate@1.yaml": &fstest.MapFile{Data: setBody("the shipped library")}}
}

func writeSet(t *testing.T, dir string, from string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool_gate@1.yaml"), setBody(from), 0o644); err != nil {
		t.Fatalf("write %s: %v", dir, err)
	}
}

func TestTheQuestionComesFromTheNamedDirectoryAndNotTheWorkingOne(t *testing.T) {
	named, standing := t.TempDir(), t.TempDir()
	writeSet(t, filepath.Join(named, sys.StateDirName, "questions"), "the named directory")
	writeSet(t, filepath.Join(standing, sys.StateDirName, "questions"), "the directory the process stood in")
	t.Chdir(standing)

	layers, err := Layers(shippedSet(), named)
	if err != nil {
		t.Fatalf("layers in %s: %v", named, err)
	}
	set, _, err := Resolve("tool_gate", layers)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	t.Logf("project layer %q, wording %q", layers[2].Origin, set.Questions[0].Instructions)
	if got := set.Questions[0].Instructions; got != "from the named directory" {
		t.Fatalf("the wording came from %s, the directory the process stood in, and not from %s: %q", standing, named, got)
	}
}

func TestTheProjectQuestionBeatsTheGlobalWhichBeatsTheShipped(t *testing.T) {
	layers, err := Layers(shippedSet(), "")
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected three layers, got %d", len(layers))
	}
	names := []string{layers[0].Name, layers[1].Name, layers[2].Name}
	t.Logf("layers %v origins %q %q %q", names, layers[0].Origin, layers[1].Origin, layers[2].Origin)
	if names[0] != "library" || names[1] != "global" || names[2] != "project" {
		t.Fatalf("the stack is not library, global, project: %v", names)
	}
	for _, layer := range layers {
		if filepath.Base(layer.Origin) != "questions" {
			t.Fatalf("the %s layer is not a questions directory: %q", layer.Name, layer.Origin)
		}
	}

	set, _, err := Resolve("tool_gate", layers)
	if err != nil {
		t.Fatalf("shipped alone: %v", err)
	}
	if got := set.Questions[0].Instructions; got != "from the shipped library" {
		t.Fatalf("with nothing overriding it the shipped file lost: %q", got)
	}

	writeSet(t, layers[1].Origin, "the global directory")
	t.Cleanup(func() { _ = os.RemoveAll(layers[1].Origin) })
	if set, _, err = Resolve("tool_gate", layers); err != nil {
		t.Fatalf("with the global file: %v", err)
	}
	if got := set.Questions[0].Instructions; got != "from the global directory" {
		t.Fatalf("the global directory did not beat the shipped library: %q", got)
	}

	writeSet(t, layers[2].Origin, "the project directory")
	t.Cleanup(func() { _ = os.RemoveAll(layers[2].Origin) })
	if set, _, err = Resolve("tool_gate", layers); err != nil {
		t.Fatalf("with the project file: %v", err)
	}
	if got := set.Questions[0].Instructions; got != "from the project directory" {
		t.Fatalf("the project directory did not beat the global one: %q", got)
	}
}
