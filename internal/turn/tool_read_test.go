package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func readAt(t *testing.T, root string, args string) (Result, error) {
	t.Helper()
	tool, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}
	return tool.Run(context.Background(), json.RawMessage(args))
}

func writeUnder(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadRepairsAPathWhenOneFileUnderTheRootCarriesThatName(t *testing.T) {
	root := t.TempDir()
	writeUnder(t, root, "server/routes.ts", "the routes")

	result, err := readAt(t, root, `{"path":"routes.ts"}`)
	if err != nil {
		t.Fatalf("read on a path with one candidate: %v", err)
	}
	if !strings.HasPrefix(result.Content, "repaired: ") {
		t.Fatalf("the result does not open by saying it repaired the path: %q", result.Content)
	}
	if !strings.Contains(result.Content, "server/routes.ts") || !strings.HasSuffix(result.Content, "the routes") {
		t.Fatalf("the repaired read neither names the file it ran on nor carries its body: %q", result.Content)
	}
	t.Logf("%s", result.Content)
}

func TestReadRefusesAPathTwoFilesUnderTheRootCouldMeanAndNamesBoth(t *testing.T) {
	root := t.TempDir()
	writeUnder(t, root, "server/routes.ts", "the server routes")
	writeUnder(t, root, "client/routes.ts", "the client routes")

	_, err := readAt(t, root, `{"path":"routes.ts"}`)
	if err == nil {
		t.Fatal("read repaired a path two files could have meant, and a repair is only made when it is the only candidate")
	}
	for _, want := range []string{"client/routes.ts", "server/routes.ts"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %s: %v", want, err)
		}
	}
	t.Logf("%v", err)
}

func TestReadSaysNothingCarriesTheNameWhenTheRootHoldsNoCandidate(t *testing.T) {
	root := t.TempDir()
	writeUnder(t, root, "server/routes.ts", "the routes")

	_, err := readAt(t, root, `{"path":"handlers.ts"}`)
	if err == nil || !strings.Contains(err.Error(), "nothing there is named handlers.ts") {
		t.Fatalf("a path nothing matches has to say so rather than repair: %v", err)
	}
	t.Logf("%v", err)
}

func TestReadRepairsALineRangeAndStillNamesTheSpan(t *testing.T) {
	root := t.TempDir()
	writeUnder(t, root, "server/routes.ts", "one\ntwo\nthree\n")

	result, err := readAt(t, root, `{"path":"routes.ts","start_line":2,"end_line":3}`)
	if err != nil {
		t.Fatalf("read a range on a repaired path: %v", err)
	}
	if !strings.HasPrefix(result.Content, "repaired: ") || !strings.Contains(result.Content, "lines 2-3 of 3") {
		t.Fatalf("a repaired range read has to carry both the repair and the span: %q", result.Content)
	}
	if !strings.HasSuffix(result.Content, "two\nthree") {
		t.Fatalf("the repaired range read carries %q", result.Content)
	}
	t.Logf("%s", result.Content)
}

func TestSymbolsIsBatchedWithTheOtherReadOnlyCalls(t *testing.T) {
	registry := NewRegistry(
		&stubTool{name: "symbols"},
		&stubTool{name: "glob"},
		&stubTool{name: "write"},
	)
	calls := []llm.ToolCall{{Name: "symbols"}, {Name: "glob"}, {Name: "write"}}
	if width := registry.parallelPrefix(calls); width != 2 {
		t.Fatalf("the parallel prefix is %d calls wide, want symbols and glob together and write on its own", width)
	}
	if !readOnly("symbols") {
		t.Fatal("symbols is not read only, so a batch would stop at it")
	}
}
