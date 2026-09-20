package turn

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"boji/internal/llm"
	"boji/internal/recall"
)

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
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshaling the row: %v", err)
	}
	var read Row
	if err := json.Unmarshal(body, &read); err != nil {
		t.Fatalf("reading the row back as JSON: %v", err)
	}
	return read
}

func TestATurnRowFromTheRealLoopSurvivesJSON(t *testing.T) {
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

type toolOfferRecordingModel struct {
	inner   stubModel
	offered []string
}

func (m *toolOfferRecordingModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.offered = m.offered[:0]
	for _, tool := range request.Tools {
		m.offered = append(m.offered, tool.Name)
	}
	return m.inner.Ask(ctx, request)
}

func runATurnThatReadsALargeFile(t *testing.T, dir string, truncate bool) (Row, string, []string) {
	t.Helper()
	body := strings.Repeat("a line of the large file the model asked to read\n", 400)
	tool := &stubTool{name: "read", result: Result{Content: body, Command: "read big.txt"}}
	model := &toolOfferRecordingModel{inner: stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"big.txt"}`)}),
		messageDecision(),
	}}}

	config := baseConfig(t, model, NewRegistry(tool))
	config.ArtifactDir = dir
	config.TruncateResults = truncate
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	return row, body, model.offered
}

func TestTheLoopPutsAnOversizeResultBehindAHandleOnTheStepRow(t *testing.T) {
	dir := t.TempDir()
	row, body, offered := runATurnThatReadsALargeFile(t, dir, false)
	call := writeAndReadBack(t, row).Steps[0].ToolCalls[0]

	if call.ResultHandle == "" {
		t.Fatalf("the step row carries no handle for a %d byte result: %+v", call.ResultBytes, call)
	}
	if call.ResultHandleError != "" {
		t.Fatalf("storing the result failed: %s", call.ResultHandleError)
	}
	if call.ResultBytes != len(body) || call.RenderedBytes >= call.ResultBytes {
		t.Fatalf("result_bytes %d rendered_bytes %d, want the whole %d bytes counted and fewer rendered",
			call.ResultBytes, call.RenderedBytes, len(body))
	}
	if !slices.Contains(offered, "artifact_fetch") {
		t.Fatalf("the model was offered %v, so it got a handle it cannot read", offered)
	}

	stored, err := recall.NewStore(dir).Fetch(call.ResultHandle)
	if err != nil {
		t.Fatalf("the handle on the step row does not lead back to a stored result: %v", err)
	}
	if string(stored) != body {
		t.Fatalf("the stored result is %d bytes, the tool returned %d", len(stored), len(body))
	}
}

func TestTheOffArmTruncatesAndLeavesNoHandle(t *testing.T) {
	row, body, offered := runATurnThatReadsALargeFile(t, t.TempDir(), true)
	call := writeAndReadBack(t, row).Steps[0].ToolCalls[0]

	if call.ResultHandle != "" {
		t.Fatalf("the off arm produced handle %q", call.ResultHandle)
	}
	if call.ResultBytes != len(body) || call.RenderedBytes >= call.ResultBytes {
		t.Fatalf("the off arm rendered %d of %d bytes, want the middle cut", call.RenderedBytes, call.ResultBytes)
	}
	if slices.Contains(offered, "artifact_fetch") {
		t.Fatalf("the off arm offered artifact_fetch, and it stores nothing to fetch: %v", offered)
	}
}

func fullyPopulatedRow() Row {
	ok := 0
	return Row{
		ID:     "turn-fixture-0001",
		Schema: SchemaVersion,
		Task:   "add a route",
		Wire:   "anthropic",
		Model:  "anthropic/claude-fable-5.1",
		Spend:  SpendAPIKey,
		Root:   "turn-fixture-0000",
		Steps: []StepRow{
			{
				Index: 1,
				ToolCalls: []ToolCallRow{
					{
						Tool:              "bash",
						Args:              json.RawMessage(`{"command":"npm test"}`),
						Command:           "npm test",
						ExitCode:          &ok,
						ResultBytes:       120,
						RenderedBytes:     120,
						ResultHash:        "deadbeef",
						ResultHandle:      "2a77523db96ec6926b76bc7786734f70",
						ResultHandleError: "recorded on the same call to prove a failed store write survives the round trip",

						ParallelBatch:  2,
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
				Occupancy: &Occupancy{
					Identity:   1200,
					Facts:      340,
					WorkingSet: 19000,
					Recent:     31483,
					Target:     50000,
				},
				Bands: &recall.Bands{Identity: 4000, Facts: 0, WorkingSet: 16000, Recent: 30000},
				Fork: &Fork{
					Step:          1,
					Kind:          ForkContinuation,
					Into:          "turn-fixture-0001-f2",
					TokensBefore:  52023,
					TokensAfter:   1204,
					BlockedMicros: 318,
					Carry: recall.Carry{
						Text: "this session continues one that reached its context budget and ended.",
						Results: []recall.CarriedResult{{
							Tool:   "bash",
							Key:    `bash {"command":"npm test"}`,
							Bytes:  20000,
							Handle: "e4c8c5f23f8eca8498d82e4ba0eb3942",
						}},
					},
				},
				Compaction: &Compaction{
					Step:         1,
					TokensBefore: 52023,
					TokensAfter:  43475,
					Drops: []recall.Drop{{
						Step:        1,
						Tool:        "bash",
						Handle:      "e4c8c5f23f8eca8498d82e4ba0eb3942",
						Bytes:       20000,
						TokensFreed: 8548,
						Reason:      recall.DroppedSuperseded,
					}},
				},
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
		ForkedFrom:   "turn-fixture-0000",
		ForkedInto:   "turn-fixture-0001-f2",
		ForkKind:     ForkContinuation,
		Warnings:     []string{"recorded so the round trip proves a warning on the row itself survives"},
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
