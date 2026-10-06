package turn

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"tofu/internal/konst"
)

func renderLines(t *testing.T, tool, args, content string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	rendered, handle, err := artifacts.Render(tool, json.RawMessage(args), content, konst.TurnResultBytesCap)
	if err != nil {
		t.Fatal(err)
	}
	return rendered, handle, dir
}

func TestLineCapLeavesALineExactlyAtTheWidth(t *testing.T) {
	content := "first\r\n" + strings.Repeat("x", konst.TurnResultLineWidth) + "\r\nlast"
	if rendered, handle, _ := renderLines(t, "bash", `{}`, content); rendered != content || handle != "" {
		t.Errorf("a line exactly %d bytes wide changed or was stored: handle %q\n%.200q", konst.TurnResultLineWidth, handle, rendered)
	}
}

func TestLineCapKeepsBothEndsTheCRLFAndTheWholeOutput(t *testing.T) {
	long := "A" + strings.Repeat("x", 2000) + "Z"
	content := "first\n" + long + "\r\nlast\n"
	rendered, handle, dir := renderLines(t, "bash", `{}`, content)
	want := "\nfirst\n" + long[:384] + "…(1234 characters cut)…" + long[len(long)-384:] + "\r\nlast\n"
	if handle == "" || !strings.HasSuffix(rendered, want) {
		t.Fatalf("handle %q, the cut result ends\n%q\nwant it to end\n%q", handle, rendered[max(0, len(rendered)-len(want)):], want)
	}
	if !strings.Contains(rendered, "holds this result whole: 2015 bytes,") {
		t.Errorf("the header does not name the whole stored output: %.300q", rendered)
	}
	if middle := fetchAt(t, dir, rendered, 1000); middle != content[1000:1192] {
		t.Errorf("the artifact does not hold the uncut line: %q", middle)
	}
}

func TestLineCapCutsOnACharacterAndCountsCharacters(t *testing.T) {
	long := "a" + strings.Repeat("é", 1000)
	rendered, _, _ := renderLines(t, "bash", `{}`, long)
	if !utf8.ValidString(rendered) || !strings.Contains(rendered, "…(617 characters cut)…") {
		t.Errorf("a cut through two-byte characters reached the model as\n%q", rendered[max(0, len(rendered)-900):])
	}
}

func TestLineCapNeverSplitsAnEscapeSequence(t *testing.T) {
	long := strings.Repeat("x", 380) + "\x1b[1;31m" + strings.Repeat("x", 2000) + "\x1b[0m" + strings.Repeat("y", 382)
	rendered, _, _ := renderLines(t, "bash", `{}`, long)
	want := "\n" + strings.Repeat("x", 380) + "…(2011 characters cut)…" + strings.Repeat("y", 382)
	if !strings.HasSuffix(rendered, want) {
		t.Errorf("an escape at the cut reached the model as\n%q", rendered[max(0, len(rendered)-len(want)):])
	}
}

func TestLineCapCutsAResultThatIsOneHugeLine(t *testing.T) {
	rendered, _, dir := renderLines(t, "read", `{"path":"app.min.js"}`, strings.Repeat("x", 200000))
	_, body, _ := strings.Cut(rendered, "\n")
	if strings.Contains(body, "\n") || !strings.Contains(body, "…(199232 characters cut)…") || len(body) > 1024 {
		t.Errorf("a 200000 byte line reached the model as %d bytes: %.120q", len(body), body)
	}
	if middle := fetchAt(t, dir, rendered, 100000); middle != strings.Repeat("x", 192) {
		t.Errorf("the artifact does not hold the whole line: %q", middle)
	}
}

func TestLineCapSkipsTheToolsThatAskedForTheLine(t *testing.T) {
	content := "head\n" + strings.Repeat("q", 3000) + "\ntail"
	for _, call := range []struct{ tool, args string }{
		{"artifact_fetch", `{"handle":"00","offset":0,"length":3000}`},
		{"search", `{"pattern":"q"}`},
		{"read", `{"path":"a.js","start_line":2,"end_line":2}`},
		{"read", `{"path":"a.js","end_line":3}`},
	} {
		if rendered, handle, _ := renderLines(t, call.tool, call.args, content); rendered != content || handle != "" {
			t.Errorf("%s %s cut a line it asked for: %.200q", call.tool, call.args, rendered)
		}
	}
}

func TestLineCapReachesTheModelThroughTheLoop(t *testing.T) {
	rendered, _ := runOneTool(t, newBash(t), `{"command":"printf 'top\\n'; head -c 200000 /dev/zero | tr '\\0' x; printf '\\nbottom\\n'"}`, false)
	if !strings.Contains(rendered, "\ntop\n") || !strings.Contains(rendered, "\nbottom") || !strings.Contains(rendered, "characters cut)…") {
		t.Errorf("a bash call printing a 200000 byte line between two short ones reached the model as\n%.1500q", rendered)
	}
}
