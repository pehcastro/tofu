package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
)

func panicOf(call func()) (message string) {
	defer func() {
		if raised := recover(); raised != nil {
			message = fmt.Sprint(raised)
		}
	}()
	call()
	return ""
}

func TestRefusedWhyNamesEveryLedgerVerdict(t *testing.T) {
	request := GateRequest{TurnID: "turn-1", Tool: "write"}
	handled := map[ledger.Verdict]bool{
		ledger.VerdictUnset: true,
		ledger.VerdictAllow: true,
		ledger.VerdictAsk:   true,
		ledger.VerdictDeny:  true,
	}
	for _, v := range ledger.AllVerdicts() {
		if !handled[v] {
			t.Fatalf("%s carries no expected behaviour in this test, so a new ledger verdict can reach refusedWhy untested", v)
		}
		decision := GateDecision{Verdict: v}
		raised := panicOf(func() {
			refusedWhy(context.Background(), nil, request, decision, "")
		})
		if raised != "" {
			t.Errorf("%s has no case in refusedWhy: %s", v, raised)
		}
	}
}

func TestAllGateModesAreNamed(t *testing.T) {
	for _, m := range AllGateModes() {
		if raised := panicOf(func() { _ = m.String() }); raised != "" {
			t.Errorf("gate mode %d has no case in String: %s", int(m), raised)
		}
	}
}

func TestAllPersonAnswersAreNamed(t *testing.T) {
	for _, a := range AllPersonAnswers() {
		if raised := panicOf(func() { _ = a.allows() }); raised != "" {
			t.Errorf("person answer %d has no case in allows: %s", int(a), raised)
		}
		if raised := panicOf(func() { _ = a.Outcome() }); raised != "" {
			t.Errorf("person answer %d has no case in Outcome: %s", int(a), raised)
		}
	}
}

func TestPersonAnswerOutcomeMatchesWhatWasAnswered(t *testing.T) {
	cases := []struct {
		answer PersonAnswer
		detail string
	}{
		{PersonDenied, "deny"},
		{PersonAllowedOnce, "allow"},
		{PersonAlwaysHere, "allow"},
	}
	for _, c := range cases {
		out := c.answer.Outcome()
		if out.Kind != OutcomeKindGateAnswer {
			t.Errorf("%d: expected kind %q, got %q", c.answer, OutcomeKindGateAnswer, out.Kind)
		}
		if out.Detail != c.detail {
			t.Errorf("%d: expected detail %q, got %q", c.answer, c.detail, out.Detail)
		}
	}
}

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

func gatedConfig(t *testing.T, gate Gate, tool Tool, model Model) Config {
	t.Helper()
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(tool),
		Gate:           gate,
		Task:           "write a file",
		Caps:           Caps{MaxSteps: 10},
		ResultBytesCap: 4096,
		ArtifactDir:    t.TempDir(),
	}
}

func TestRefusedWhyOnlyAsksThePersonWhenTheVerdictIsAsk(t *testing.T) {
	request := GateRequest{TurnID: "turn-1", Tool: "write"}
	asked := 0
	person := Person(func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
		asked++
		return PersonAllowedOnce, nil
	})
	for _, v := range []ledger.Verdict{ledger.VerdictUnset, ledger.VerdictAllow, ledger.VerdictDeny} {
		refusedWhy(context.Background(), person, request, GateDecision{ID: "row-1", Verdict: v}, "")
	}
	if asked != 0 {
		t.Fatalf("the person was asked %d times for a verdict that never asks, so an unasked allow has nothing to write an outcome from", asked)
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

	row, err := Run(context.Background(), gatedConfig(t, gate, tool, model))
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

	row, err := Run(context.Background(), gatedConfig(t, gate, tool, model))
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

	row, err := Run(context.Background(), gatedConfig(t, gate, tool, model))
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

func TestRunWithoutAGateRecordsNoDecision(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}
	row, err := Run(context.Background(), gatedConfig(t, nil, tool, model))
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
