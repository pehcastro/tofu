package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type scriptedModel struct {
	calls []llm.ToolCall
	step  int
}

func (m *scriptedModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if m.step >= len(m.calls) {
		return llm.Decision{Build: "scripted", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	call := m.calls[m.step]
	m.step++
	return llm.Decision{Build: "scripted", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}}, nil
}

func seed(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("seeding %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatalf("seeding %s: %v", rel, err)
	}
}

func registry(t *testing.T, root string) turn.Registry {
	t.Helper()
	globTool, globErr := tools.NewGlob(root)
	grepTool, grepErr := tools.NewGrep(root)
	editTool, editErr := tools.NewEdit(root)
	symbolsTool, symbolsErr := tools.NewSymbols(root)
	for _, err := range []error{globErr, grepErr, editErr, symbolsErr} {
		if err != nil {
			t.Fatalf("building the tools: %v", err)
		}
	}
	return turn.NewRegistry(globTool, grepTool, editTool, symbolsTool)
}

func runCalls(t *testing.T, root string, bytesCap int, truncate bool, calls ...llm.ToolCall) turn.Row {
	t.Helper()
	row, err := turn.Run(context.Background(), turn.Config{
		Model:           &scriptedModel{calls: calls},
		Spend:           turn.SpendSubscription,
		Tools:           registry(t, root),
		Task:            "exercise the tools this ticket adds",
		ResultBytesCap:  bytesCap,
		ArtifactDir:     t.TempDir(),
		TruncateResults: truncate,
	})
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	return row
}

func loggedRows(t *testing.T, row turn.Row) []turn.ToolCallRow {
	t.Helper()
	var called []turn.ToolCallRow
	for _, step := range row.Steps {
		called = append(called, step.ToolCalls...)
	}
	for _, call := range called {
		encoded, err := json.Marshal(call)
		if err != nil {
			t.Fatalf("encoding the step row: %v", err)
		}
		t.Logf("step row: %s", encoded)
	}
	return called
}

const tasksFile = `export type Task = {
  id: number
  title: string
  done: boolean
}

export const empty: Task[] = []
`

func TestEachNewToolAppliesOnARealFileAndRoundTripsThroughTheStepRow(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "src/store.ts", tasksFile)
	seed(t, root, "node_modules/hono/index.ts", "  done: boolean\n")

	row := runCalls(t, root, konst.TurnResultBytesCap, false,
		llm.ToolCall{ID: "c1", Name: "glob", Arguments: json.RawMessage(`{"pattern":"*.ts"}`)},
		llm.ToolCall{ID: "c2", Name: "grep", Arguments: json.RawMessage(`{"pattern":"done"}`)},
		llm.ToolCall{ID: "c3", Name: "edit", Arguments: json.RawMessage(
			`{"path":"src/store.ts","old_string":"  title: string\n  done: boolean","new_string":"  title: string\n  completed: boolean"}`)},
	)
	called := loggedRows(t, row)
	if len(called) != 3 {
		t.Fatalf("expected one row per tool, got %d", len(called))
	}
	for _, call := range called {
		if call.Error != "" {
			t.Fatalf("%s failed: %s", call.Tool, call.Error)
		}
		if call.ResultBytes == 0 || call.ResultHash == "" {
			t.Fatalf("%s produced a row with no result: %+v", call.Tool, call)
		}
	}

	if !strings.Contains(called[0].Command, "glob *.ts") {
		t.Fatalf("the glob row does not name what it searched: %q", called[0].Command)
	}
	after, err := os.ReadFile(filepath.Join(root, "src", "store.ts"))
	if err != nil {
		t.Fatalf("reading the edited file: %v", err)
	}
	if !strings.Contains(string(after), "  completed: boolean") || strings.Contains(string(after), "  done: boolean") {
		t.Fatalf("the edit did not replace the line in place:\n%s", after)
	}
	if !strings.Contains(string(after), "export const empty: Task[] = []") {
		t.Fatalf("the edit lost the rest of the file:\n%s", after)
	}
}

func TestGlobAndGrepSkipNodeModulesAndSayWhenNothingMatched(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "src/app.ts", "const port = 3000\n")
	seed(t, root, "node_modules/hono/index.ts", "const port = 3000\n")
	globTool, _ := tools.NewGlob(root)
	grepTool, _ := tools.NewGrep(root)

	listed, err := globTool.Run(context.Background(), json.RawMessage(`{"pattern":"*.ts"}`))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(listed.Content, "src/app.ts") || strings.Contains(listed.Content, "node_modules") {
		t.Fatalf("glob must list the project and never node_modules, it returned:\n%s", listed.Content)
	}

	found, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"port"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(found.Content, "src/app.ts:1:const port = 3000") || strings.Contains(found.Content, "node_modules") {
		t.Fatalf("grep must report path:line:text and never node_modules, it returned:\n%s", found.Content)
	}

	empty, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"nothing here matches this"}`))
	if err != nil {
		t.Fatalf("zero matches must be an answer, not an error: %v", err)
	}
	if !strings.Contains(empty.Content, "matches no line") || !strings.Contains(empty.Content, "not a failure") {
		t.Fatalf("zero matches must say it searched and found nothing, it returned:\n%s", empty.Content)
	}
}

func TestEditTakesTheMultiLineBlockAModelActuallySendsAndRefusesAnAmbiguousOne(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "same\nmiddle\nsame\nend\n")
	editTool, err := tools.NewEdit(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}

	_, err = editTool.Run(context.Background(), json.RawMessage(
		`{"path":"a.txt","old_string":"same","new_string":"other"}`))
	if err == nil {
		t.Fatal("expected text that appears twice to be refused rather than guessed at")
	}
	if !strings.Contains(err.Error(), "appears 2 times") || !strings.Contains(err.Error(), "until it is unique") {
		t.Fatalf("the error does not tell the model how to repair the call: %v", err)
	}

	result, err := editTool.Run(context.Background(), json.RawMessage(
		`{"path":"a.txt","old_string":"middle\nsame","new_string":"middle\nother"}`))
	if err != nil {
		t.Fatalf("a multi-line block that appears once must apply: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("reading the edited file: %v", err)
	}
	if string(after) != "same\nmiddle\nother\nend\n" {
		t.Fatalf("the edit landed on the wrong occurrence:\n%s", after)
	}
	if !strings.Contains(result.Content, "-same") || !strings.Contains(result.Content, "+other") {
		t.Fatalf("the result is not a diff of what changed:\n%s", result.Content)
	}
}

func manyMatches(lines int) string {
	var body strings.Builder
	for i := 0; i < lines; i++ {
		body.WriteString("the needle is on this line and this line is long enough to add up quickly\n")
	}
	return body.String()
}

func TestALargeGrepResultGoesThroughTheArtifactHandleAndNotTruncation(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "big.txt", manyMatches(400))
	call := llm.ToolCall{ID: "c1", Name: "grep", Arguments: json.RawMessage(`{"pattern":"needle"}`)}

	stored := loggedRows(t, runCalls(t, root, 1024, false, call))[0]
	if stored.ResultHandleError != "" {
		t.Fatalf("storing the artifact failed: %s", stored.ResultHandleError)
	}
	if stored.ResultHandle == "" {
		t.Fatal("a grep result over the byte cap was rendered without an artifact handle, so the dropped bytes are unrecoverable")
	}
	if stored.ResultBytes <= stored.RenderedBytes {
		t.Fatalf("expected the rendered result to be smaller than the whole, got %d rendered of %d", stored.RenderedBytes, stored.ResultBytes)
	}

	truncated := loggedRows(t, runCalls(t, root, 1024, true, call))[0]
	if truncated.ResultHandle != "" {
		t.Fatalf("the off arm truncates and must store nothing, got handle %s", truncated.ResultHandle)
	}
	if truncated.ResultHash != stored.ResultHash {
		t.Fatal("the two arms did not see the same grep result, so the comparison says nothing")
	}
}
