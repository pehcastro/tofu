package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"boji/internal/llm"
)

func TestDirIsSessionsBesideTheLedgersOwnLog(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if filepath.Base(dir) != "sessions" {
		t.Fatalf("Dir() = %q, want a path ending in sessions", dir)
	}
	if filepath.Base(filepath.Dir(dir)) != ".boji" {
		t.Fatalf("Dir() = %q, want it under .boji, beside the ledger's own log", dir)
	}
}

type sequentialTool struct {
	name    string
	results []Result
	calls   int
}

func (t *sequentialTool) Name() string { return t.name }

func (t *sequentialTool) Definition() llm.Tool {
	return llm.Tool{Name: t.name, Description: "a stub tool", Parameters: map[string]any{"type": "object"}}
}

func (t *sequentialTool) Run(_ context.Context, _ json.RawMessage) (Result, error) {
	result := t.results[t.calls]
	t.calls++
	return result, nil
}

func runTurnWithASucceedingAndAFailingBashCall(t *testing.T) Row {
	t.Helper()
	ok, fail := 0, 1
	tool := &sequentialTool{name: "bash", results: []Result{
		{Content: "all tests passed", Command: "npm test", ExitCode: &ok},
		{Content: "1 test failed", Command: "npm test --broken", ExitCode: &fail},
	}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(
			llm.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"npm test"}`)},
			llm.ToolCall{ID: "call-2", Name: "bash", Arguments: json.RawMessage(`{"command":"npm test --broken"}`)},
		),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	return row
}

func writeAndReadBack(t *testing.T, row Row) Row {
	t.Helper()
	dir := t.TempDir()
	written, err := NewWriter(dir).Append(row)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	read, found, err := NewReader(dir).ByID(written.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !found {
		t.Fatalf("ByID did not find the row just written, id %q", written.ID)
	}
	return read
}

func TestATurnRowWrittenByTheRealLoopReadsBackByID(t *testing.T) {
	read := writeAndReadBack(t, runTurnWithASucceedingAndAFailingBashCall(t))
	if read.Outcome != OutcomeStopped {
		t.Fatalf("expected outcome stopped, got %s", read.Outcome)
	}
	if len(read.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(read.Steps))
	}
}

func TestToolEvidenceSurvivesForAFailedAndASucceededCall(t *testing.T) {
	read := writeAndReadBack(t, runTurnWithASucceedingAndAFailingBashCall(t))
	calls := read.Steps[0].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls in step 1, got %d", len(calls))
	}

	succeeded, failed := calls[0], calls[1]
	if succeeded.Command != "npm test" {
		t.Fatalf("succeeded call command = %q", succeeded.Command)
	}
	if succeeded.ExitCode == nil || *succeeded.ExitCode != 0 {
		t.Fatalf("succeeded call exit code = %+v, want 0", succeeded.ExitCode)
	}
	if failed.Command != "npm test --broken" {
		t.Fatalf("failed call command = %q", failed.Command)
	}
	if failed.ExitCode == nil || *failed.ExitCode != 1 {
		t.Fatalf("failed call exit code = %+v, want 1", failed.ExitCode)
	}
}

func fullyPopulatedRow() Row {
	ok := 0
	return Row{
		ID:     "turn-fixture-0001",
		Schema: SchemaVersion,
		Task:   "add a route",
		Model:  "anthropic/claude-fable-5.1",
		Spend:  SpendAPIKey,
		Steps: []StepRow{
			{
				Index: 1,
				ToolCalls: []ToolCallRow{
					{
						Tool:           "bash",
						Args:           json.RawMessage(`{"command":"npm test"}`),
						Command:        "npm test",
						ExitCode:       &ok,
						ResultBytes:    120,
						RenderedBytes:  120,
						ResultHash:     "deadbeef",
						GateDecisionID: "2026-09-18-deadbeef",
						GateVerdict:    "deny",
						GateError:      "recorded on the same call to prove the field round-trips",
						DurationMS:     42,
						Error:          "recorded even on a call that also carries evidence, to prove the field round-trips",
					},
				},
				AssistantText:    "checked the test output before continuing",
				StopReason:       "tool_use",
				PromptTokens:     100,
				CompletionTokens: 20,
				CacheReadTokens:  900,
				CacheWriteTokens: 40,
				CostUSD:          0.001,
				Warnings:         []string{"recorded so the round trip proves a wire warning survives"},
			},
			{
				Index:            2,
				AssistantText:    "done",
				StopReason:       "end_turn",
				PromptTokens:     50,
				CompletionTokens: 10,
				CacheReadTokens:  950,
				CostUSD:          0.0005,
			},
		},
		Outcome:      OutcomeStopped,
		TotalCostUSD: 0.0015,
		WallClockMS:  1500,
		DecisionIDs:  []string{"2026-09-18-deadbeef"},
	}
}

func fieldPaths(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return fieldPaths(t.Elem(), prefix)
	}
	if t.Kind() != reflect.Struct || t.Name() == "Time" {
		return nil
	}
	var paths []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		path := prefix + tag
		paths = append(paths, path)
		paths = append(paths, fieldPaths(field.Type, path+".")...)
	}
	return paths
}

func lookup(tree any, path string) (any, bool) {
	node := tree
	for _, key := range strings.Split(path, ".") {
		switch typed := node.(type) {
		case map[string]any:
			value, ok := typed[key]
			if !ok {
				return nil, false
			}
			node = value
		case []any:
			if len(typed) == 0 {
				return nil, false
			}
			node = typed[0]
			if m, ok := node.(map[string]any); ok {
				value, ok := m[key]
				if !ok {
					return nil, false
				}
				node = value
			}
		default:
			return nil, false
		}
	}
	return node, true
}

func TestRoundTripCarriesEveryFieldOfARow(t *testing.T) {
	read := writeAndReadBack(t, fullyPopulatedRow())

	body, err := json.Marshal(read)
	if err != nil {
		t.Fatalf("marshaling the row read back: %v", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatalf("the row read back does not parse as JSON: %v", err)
	}

	for _, path := range fieldPaths(reflect.TypeOf(Row{}), "") {
		if _, ok := lookup(tree, path); !ok {
			t.Errorf("the row carries field %q, the round trip dropped it: %s", path, body)
		}
	}
}
