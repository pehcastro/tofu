package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/search"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("seeding %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatalf("seeding %s: %v", rel, err)
	}
}

func TestTheGraphCarriesTheDefinitionItsCalleesAndEveryCallSite(t *testing.T) {
	root := t.TempDir()
	write(t, root, "service.go", "package auth\n\nfunc authenticate(user string) bool {\n\treturn verify(user)\n}\n")
	write(t, root, "login.go", "package auth\n\nfunc login(user string) bool {\n\treturn authenticate(user)\n}\n")
	write(t, root, "broken.go", "package auth\n\nfunc (\n")
	write(t, root, "notes.md", "authenticate is not go\n")
	files := []string{"service.go", "login.go", "broken.go", "notes.md"}

	graph, err := search.Symbols(root, files, "authenticate")
	if err != nil {
		t.Fatalf("symbols: %v", err)
	}
	if graph.Parsed != 2 || graph.Unparsed != 1 {
		t.Fatalf("expected 2 parsed go files and 1 that did not, got %d and %d", graph.Parsed, graph.Unparsed)
	}
	if len(graph.Definitions) != 1 {
		t.Fatalf("expected one definition, got %+v", graph.Definitions)
	}
	definition := graph.Definitions[0]
	if definition.Path != "service.go" || definition.Kind != search.KindFunc || definition.FirstLine != 3 || definition.LastLine != 5 {
		t.Fatalf("the definition does not carry where it is: %+v", definition)
	}
	if len(definition.Calls) != 1 || definition.Calls[0] != "verify" {
		t.Fatalf("the definition does not carry what it calls: %+v", definition.Calls)
	}
	if len(graph.Callers) != 1 {
		t.Fatalf("expected one call site, got %+v", graph.Callers)
	}
	if got := graph.Callers[0]; got.Path != "login.go" || got.Line != 4 || got.Within != "login" {
		t.Fatalf("the call site does not carry the function it sits in: %+v", got)
	}
}

func TestAMethodIsFoundThroughItsReceiverAndACallThroughAValueIsNot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "store.go", "package store\n\ntype Store struct{}\n\nfunc (s Store) Close() error { return nil }\n\n"+
		"func shut(s Store, f func() error) error {\n\t_ = s.Close()\n\treturn f()\n}\n")

	graph, err := search.Symbols(root, []string{"store.go"}, "Close")
	if err != nil {
		t.Fatalf("symbols: %v", err)
	}
	if len(graph.Definitions) != 1 || graph.Definitions[0].Symbol != "Store.Close" || graph.Definitions[0].Kind != search.KindMethod {
		t.Fatalf("a method must come back named by its receiver: %+v", graph.Definitions)
	}
	if len(graph.Callers) != 1 || graph.Callers[0].Within != "shut" {
		t.Fatalf("expected the one call site inside shut: %+v", graph.Callers)
	}
	if got := graph.Text; !strings.Contains(got, "degraded name_matched") {
		t.Fatalf("the call through f is invisible here and the answer does not say so:\n%s", got)
	}
}
