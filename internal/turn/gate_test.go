package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"boji/internal/judge/ledger"
	"boji/internal/llm"
)

type stubGate struct {
	verdicts []ledger.Verdict
	reason   *ledger.Reason
	err      error
	requests []GateRequest
}

func gateSaying(verdicts ...ledger.Verdict) *stubGate { return &stubGate{verdicts: verdicts} }

func (g *stubGate) Decide(_ context.Context, request GateRequest) (GateDecision, error) {
	g.requests = append(g.requests, request)
	if g.err != nil {
		return GateDecision{Verdict: ledger.VerdictAsk}, g.err
	}
	verdict := g.verdicts[min(len(g.requests), len(g.verdicts))-1]
	return GateDecision{ID: "row-" + strconv.Itoa(len(g.requests)), Verdict: verdict, Reason: g.reason}, nil
}

func gatedConfig(gate Gate, tool Tool, model Model) Config {
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(tool),
		Gate:           gate,
		Task:           "write a file",
		Caps:           Caps{MaxSteps: 10, MaxDecisions: 10},
		ResultBytesCap: 4096,
	}
}

func TestRunAsksTheGateBeforeEveryToolCallAndRecordsTheDecision(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	gate := gateSaying(ledger.VerdictAllow)
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		toolCallDecision(llm.ToolCall{ID: "c2", Name: "write", Arguments: json.RawMessage(`{"path":"b.txt"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), gatedConfig(gate, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(gate.requests) != 2 {
		t.Fatalf("expected one decision per tool call, got %d", len(gate.requests))
	}
	first := gate.requests[0]
	if first.Tool != "write" || string(first.Args) != `{"path":"a.txt"}` || first.TurnID != row.ID || first.Task != "write a file" {
		t.Fatalf("the gate saw %+v, and the turn is %s", first, row.ID)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateDecisionID != "row-1" || call.GateVerdict != "allow" || call.GateError != "" {
		t.Fatalf("the tool call row is %+v", call)
	}
	if len(row.DecisionIDs) != 2 || row.DecisionIDs[1] != "row-2" {
		t.Fatalf("expected the turn row to name both decisions, got %v", row.DecisionIDs)
	}
}

func TestUnderShadowADenyStillRunsTheCallAndOnlyRecordsTheVerdict(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	gate := gateSaying(ledger.VerdictDeny)
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), gatedConfig(gate, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("a deny must not stop the step: the tool ran %d times", tool.calls)
	}
	if row.Steps[0].ToolCalls[0].GateVerdict != "deny" {
		t.Fatalf("expected the deny recorded on the tool call, got %+v", row.Steps[0].ToolCalls[0])
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected outcome stopped, got %s", row.Outcome)
	}
}

func TestUnderShadowAGateThatErrorsStillRunsTheCall(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	gate := &stubGate{err: errors.New("the route timed out")}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), gatedConfig(gate, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateError != "the route timed out" || call.GateVerdict != "ask" {
		t.Fatalf("expected a failed decision to record ask and the error, got %+v", call)
	}
	if call.GateDecisionID != "" {
		t.Fatalf("a failed decision has no row to point at, got %q", call.GateDecisionID)
	}
	if tool.calls != 1 {
		t.Fatalf("expected the call to run anyway, ran %d times", tool.calls)
	}
}

func TestRunStopsAtTheDecisionCapWithItsOwnOutcome(t *testing.T) {
	gate := gateSaying(ledger.VerdictAllow)
	config := gatedConfig(gate, &stubTool{name: "noop", result: Result{Content: "ok"}}, alwaysToolCallModel(10))
	config.Caps = Caps{MaxSteps: 10, MaxDecisions: 3}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeDecisionCap {
		t.Fatalf("expected outcome decision_cap, got %s", row.Outcome)
	}
	if len(gate.requests) != 3 {
		t.Fatalf("expected exactly 3 decisions, got %d", len(gate.requests))
	}
	if len(row.DecisionIDs) != 3 {
		t.Fatalf("expected 3 decision ids on the turn row, got %v", row.DecisionIDs)
	}
}

func TestRunWithoutAGateRecordsNoDecision(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}
	row, err := Run(context.Background(), gatedConfig(nil, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateDecisionID != "" || call.GateVerdict != "" || call.GateError != "" {
		t.Fatalf("expected no gate fields without a gate, got %+v", call)
	}
	if row.DecisionIDs != nil {
		t.Fatalf("expected no decision ids, got %v", row.DecisionIDs)
	}
}
