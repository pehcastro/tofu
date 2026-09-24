package turn

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestALineRangeReadThatIsCutByTheByteCapStillReportsTheRealTotal(t *testing.T) {
	root := t.TempDir()
	const lineCount = 5000
	var body strings.Builder
	for i := 1; i <= lineCount; i++ {
		fmt.Fprintf(&body, "line %d, padded so the whole file is large %s\n", i, strings.Repeat("x", 40))
	}
	writeUnder(t, root, "big.txt", body.String())

	result, err := readAt(t, root, `{"path":"big.txt","start_line":1,"end_line":5000}`)
	if err != nil {
		t.Fatalf("reading the full range: %v", err)
	}
	if len(result.Content) >= body.Len() {
		t.Fatalf("the result is %d bytes and the file is %d: this test needs the cap to actually cut something", len(result.Content), body.Len())
	}
	want := fmt.Sprintf("lines 1-%d of %d", lineCount, lineCount)
	if !strings.Contains(result.Content, want) {
		t.Fatalf("a result cut by the byte cap no longer reports the real total: wanted %q in %q", want, result.Content[:min(200, len(result.Content))])
	}
}

func TestReadToolReturnsANamedLineRangeSoTheModelNeedNotShellOutToSed(t *testing.T) {
	root := seedRecordedFixture(t)
	result, err := readAt(t, root, `{"path":"src/app.test.ts","start_line":11,"end_line":14}`)
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	want := "src/app.test.ts lines 11-14 of 15\n" +
		"  it('rejects an over-long title with 400', async () => {\n" +
		"    const res = await postJson('/tasks', { title: 'x'.repeat(201) })\n" +
		"    expect(res.status).toBe(400)\n" +
		"  })"
	if result.Content != want {
		t.Fatalf("read returned\n%q\nwanted\n%q", result.Content, want)
	}
	if result.Command != "src/app.test.ts lines 11-14 of 15" {
		t.Fatalf("the step row recorded %q", result.Command)
	}
}

func TestReadToolClampsAnEndLinePastTheLastLineAndRefusesAStartLinePastIt(t *testing.T) {
	root := seedRecordedFixture(t)
	result, err := readAt(t, root, `{"path":"src/app.test.ts","start_line":14,"end_line":900}`)
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	if !strings.HasPrefix(result.Content, "src/app.test.ts lines 14-15 of 15\n") {
		t.Fatalf("read returned %q, wanted the clamped range named in the first line", result.Content)
	}

	_, err = readAt(t, root, `{"path":"src/app.test.ts","start_line":900}`)
	if err == nil {
		t.Fatal("expected a start_line past the end of the file to fail rather than return nothing")
	}
	if !strings.Contains(err.Error(), "has 15 lines") {
		t.Fatalf("the error did not say how long the file is: %v", err)
	}
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
