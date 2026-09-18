package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"boji/internal/llm"
)

func baseConfig(t *testing.T, model Model, tools Registry) Config {
	t.Helper()
	return Config{
		Model:          model,
		Tools:          tools,
		Task:           "say pong",
		Caps:           Caps{MaxSteps: 10},
		ResultBytesCap: 4096,
	}
}

func TestRunDrivesTheLoopToCompletionAndRecordsEveryStepAndCall(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected outcome stopped, got %s", row.Outcome)
	}
	if len(row.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(row.Steps))
	}
	if len(row.Steps[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on step 1, got %d", len(row.Steps[0].ToolCalls))
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Tool != "read" || call.Command != "read a.txt" {
		t.Fatalf("tool call row is %+v", call)
	}
	if row.Steps[1].AssistantText != "done" {
		t.Fatalf("expected the final step to carry the assistant's text, got %+v", row.Steps[1])
	}
	if tool.calls != 1 {
		t.Fatalf("expected the tool to run once, ran %d times", tool.calls)
	}
}

func TestRunRefusesAMalformedToolCallAtTheBoundaryAndContinues(t *testing.T) {
	tool, err := NewReadTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}

	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(row.Steps) != 2 {
		t.Fatalf("expected the turn to continue past the malformed call, got %d steps", len(row.Steps))
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Error == "" {
		t.Fatalf("expected the tool call row to carry an error, got %+v", call)
	}
	if !strings.Contains(call.Error, "path is required") {
		t.Fatalf("error did not name what was wrong: %q", call.Error)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected the turn to still reach stopped, got %s", row.Outcome)
	}
}

func TestRunRefusesAnUnknownTool(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "delete_everything", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry()))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if !strings.Contains(call.Error, "unknown tool") {
		t.Fatalf("expected an unknown tool error, got %q", call.Error)
	}
}

func TestRunRecordsACommandsExitCodeAndCommandString(t *testing.T) {
	code := 3
	tool := &stubTool{name: "bash", result: Result{Content: "did the thing", Command: "exit 3", ExitCode: &code}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"exit 3"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Command != "exit 3" {
		t.Fatalf("expected the command string to be recorded, got %q", call.Command)
	}
	if call.ExitCode == nil || *call.ExitCode != 3 {
		t.Fatalf("expected exit code 3 recorded, got %+v", call.ExitCode)
	}
}

func TestRunSurfacesAModelError(t *testing.T) {
	sentinel := &errorModel{err: context.DeadlineExceeded}
	_, err := Run(context.Background(), baseConfig(t, sentinel, NewRegistry()))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestPackageMakesNoNetworkCallOfItsOwn(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing package files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no source files found, the test found nothing")
	}
	forbidden := []string{`"net/http"`, `"net"`}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, imp := range forbidden {
			if strings.Contains(string(data), imp) {
				t.Fatalf("%s imports %s: the turn loop must call the model only through the Model interface", file, imp)
			}
		}
	}
}
