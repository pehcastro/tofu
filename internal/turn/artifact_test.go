package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

const bigLinePad = "............................................"

func runOneTool(t *testing.T, tool Tool, args string, truncate bool) (string, string) {
	t.Helper()
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: tool.Name(), Arguments: json.RawMessage(args)}),
		messageDecision(),
	}}
	dir := t.TempDir()
	config := Config{Model: model, Spend: SpendSubscription, Task: "run " + tool.Name(), ResultBytesCap: konst.TurnResultBytesCap,
		ArtifactDir: dir, NoLastWord: true, TruncateResults: truncate, Tools: NewRegistry(tool)}
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	results := toolResults(model.requests[len(model.requests)-1].Messages)
	if len(results) != 1 {
		t.Fatalf("the last request holds %d tool results, want 1", len(results))
	}
	return results[0], dir
}

func readBigFile(t *testing.T, args string, truncate bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	var body strings.Builder
	for line := 1; line <= 1600; line++ {
		fmt.Fprintf(&body, "line %05d of 1600 %s\n", line, bigLinePad)
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	return runOneTool(t, read, args, truncate)
}

func fetchAt(t *testing.T, dir, rendered string, offset int) string {
	t.Helper()
	words := strings.Fields(rendered)
	if len(words) < 2 || words[0] != "artifact" {
		t.Fatalf("the result did not render an artifact handle:\n%.300s", rendered)
	}
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	fetched, err := artifacts.FetchTool().Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"handle":%q,"offset":%d,"length":192}`, words[1], offset)))
	if err != nil {
		t.Fatalf("artifact_fetch at %d: %v", offset, err)
	}
	return fetched.Content
}

func TestAReadOverTheCapIsStoredWholeSoItsMiddleCanBeFetched(t *testing.T) {
	rendered, dir := readBigFile(t, `{"path":"big.txt"}`, false)
	if len(rendered) > 2*1024 || !strings.Contains(rendered, "holds this result whole: 102400 bytes,") {
		t.Errorf("the model got %d bytes for a 100 KB read, want a head, a tail and a handle naming all 102400:\n%.300s", len(rendered), rendered)
	}
	if middle := fetchAt(t, dir, rendered, 51200); !strings.HasPrefix(middle, "line 00801 of 1600 ") {
		t.Errorf("the fetch at the middle of the file returned %q, want line 801", middle)
	}
}

func TestARangedReadOverTheCapIsStoredWholeToo(t *testing.T) {
	rendered, dir := readBigFile(t, `{"path":"big.txt","start_line":1,"end_line":1200}`, false)
	if middle := fetchAt(t, dir, rendered, 38400); !strings.Contains(middle, "line 00601 of 1600 ") {
		t.Errorf("the fetch inside a 1200 line range returned %q, want line 601", middle)
	}
}

func TestAReadUnderTheCapReachesTheModelByteForByte(t *testing.T) {
	rendered, _ := readBigFile(t, `{"path":"big.txt","start_line":10,"end_line":12}`, false)
	want := "big.txt lines 10-12 of 1600\nline 00010 of 1600 " + bigLinePad + "\nline 00011 of 1600 " + bigLinePad + "\nline 00012 of 1600 " + bigLinePad
	if rendered != want {
		t.Errorf("a three line read reached the model as\n%q\nwant\n%q", rendered, want)
	}
}

func TestWithHandlesOffAReadIsCutOnceAtTheCap(t *testing.T) {
	rendered, _ := readBigFile(t, `{"path":"big.txt"}`, true)
	if cuts := strings.Count(rendered, "dropped from the middle"); cuts != 1 || len(rendered) > konst.TurnResultBytesCap+len(droppedMarker)+16 {
		t.Errorf("with handles off a 100 KB read reached the model as %d bytes with %d cut markers, want one cut to the %d byte cap",
			len(rendered), cuts, konst.TurnResultBytesCap)
	}
}

func TestABashResultUnderItsHoldIsStoredWholeSoItsMiddleCanBeFetched(t *testing.T) {
	rendered, dir := runOneTool(t, newBash(t), `{"command":"seq 1 12000"}`, false)
	if !strings.Contains(rendered, "holds this result whole: 60894 bytes,") {
		t.Errorf("a 60894 byte seq rendered as:\n%.300s", rendered)
	}
	if middle := fetchAt(t, dir, rendered, 30447); !strings.Contains(middle, "\n6312\n") {
		t.Errorf("the fetch at the middle of seq 1 12000 returned %q, want the numbers around 6000", middle)
	}
}

func TestABashResultOverItsHoldDoesNotClaimToBeWhole(t *testing.T) {
	rendered, dir := runOneTool(t, newBash(t), `{"command":"seq 1 40000","timeout_ms":-1}`, false)
	header, _, _ := strings.Cut(rendered, "\n")
	if strings.Contains(header, "whole") || !strings.Contains(rendered, "dropped as they arrived") {
		t.Errorf("a seq bash dropped bytes from rendered as:\n%.600s", rendered)
	}
	if stored := fetchAt(t, dir, rendered, 0); !strings.HasPrefix(stored, "bash: the command printed 228894 bytes") {
		t.Errorf("the artifact does not open with the drop note: %q", stored)
	}
	if seam := fetchAt(t, dir, rendered, 32900); !strings.Contains(seam, "bytes dropped here as they arrived") {
		t.Errorf("the fetch across the seam of the held halves returned %q, want the drop marker", seam)
	}
}

func TestWithHandlesOffABashResultIsCutOnceAtTheCap(t *testing.T) {
	rendered, _ := runOneTool(t, newBash(t), `{"command":"seq 1 12000"}`, true)
	if cuts := strings.Count(rendered, "dropped from the middle"); cuts != 1 || len(rendered) > konst.TurnResultBytesCap+len(droppedMarker)+16 {
		t.Errorf("with handles off a 60894 byte seq reached the model as %d bytes with %d cut markers, want one cut to the %d byte cap",
			len(rendered), cuts, konst.TurnResultBytesCap)
	}
}
