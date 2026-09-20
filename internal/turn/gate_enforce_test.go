package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"boji/internal/judge/ledger"
	"boji/internal/llm"
)

func enforced(gate Gate, tool Tool, model Model) Config {
	config := gatedConfig(gate, tool, model)
	config.GateMode = GateEnforce
	return config
}

func callThenAnswer(calls ...llm.ToolCall) *stubModel {
	decisions := make([]llm.Decision, 0, len(calls)+1)
	for _, call := range calls {
		decisions = append(decisions, toolCallDecision(call))
	}
	return &stubModel{decisions: append(decisions, messageDecision())}
}

func judgedWrite(id, path string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "write", Arguments: json.RawMessage(`{"path":"` + path + `"}`)}
}

func toolResultSeenByTheModel(t *testing.T, model *stubModel) string {
	t.Helper()
	for _, request := range model.requests {
		for _, message := range request.Messages {
			if message.Role == llm.RoleTool {
				return message.Content
			}
		}
	}
	t.Fatal("the model was never shown a tool result")
	return ""
}

func TestUnderEnforceADenyDoesNotRunTheToolAndTellsTheModelWhy(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	gate := gateSaying(ledger.VerdictDeny)
	gate.reason = &ledger.Reason{Question: "risk", Comparison: "risk_deny_at", Threshold: 2.5, Value: 3}
	model := callThenAnswer(judgedWrite("c1", "a.txt"))

	row, err := Run(context.Background(), enforced(gate, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if tool.calls != 0 {
		t.Fatalf("a deny under enforce ran the tool %d times", tool.calls)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateVerdict != string(ledger.VerdictDeny) || call.Error == "" {
		t.Fatalf("the tool call row is %+v, want the deny and the refusal on it", call)
	}
	shown := toolResultSeenByTheModel(t, model)
	for _, want := range []string{"refused", "the verdict is deny", "risk 3.00 is over risk_deny_at 2.50"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the model was told %q, which does not carry %q", shown, want)
		}
	}
	t.Logf("the model reads: %s", shown)
}

func TestARefusedCallDoesNotEndTheTurn(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	gate := gateSaying(ledger.VerdictDeny, ledger.VerdictAllow)
	model := callThenAnswer(judgedWrite("c1", "a.txt"), judgedWrite("c2", "b.txt"))

	row, err := Run(context.Background(), enforced(gate, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("the turn ended as %s, want stopped: a refusal is a result, not a failure", row.Outcome)
	}
	if tool.calls != 1 {
		t.Fatalf("the tool ran %d times, want the one call the gate allowed", tool.calls)
	}
	if len(row.Steps) != 3 {
		t.Fatalf("the turn took %d steps, want the refused call, the allowed call and the answer", len(row.Steps))
	}
	t.Logf("steps %d, refusal %q", len(row.Steps), row.Steps[0].ToolCalls[0].Error)
}

func TestUnderEnforceAGateThatErrorsRefuses(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	gate := &stubGate{err: errors.New("the route timed out")}
	model := callThenAnswer(judgedWrite("c1", "a.txt"))

	row, err := Run(context.Background(), enforced(gate, tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if tool.calls != 0 {
		t.Fatalf("a check that cannot run must refuse, and the tool ran %d times", tool.calls)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateError != "the route timed out" {
		t.Fatalf("the row does not carry what went wrong: %+v", call)
	}
	if !strings.Contains(call.Error, "a check that cannot run refuses: the route timed out") {
		t.Fatalf("the refusal does not say the check could not run: %q", call.Error)
	}
	t.Logf("the model reads: %s", toolResultSeenByTheModel(t, model))
}

func TestUnderEnforceAnAskWithNoPersonRefusesAndSaysSo(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := callThenAnswer(judgedWrite("c1", "a.txt"))

	row, err := Run(context.Background(), enforced(gateSaying(ledger.VerdictAsk), tool, model))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if tool.calls != 0 {
		t.Fatalf("an ask nobody can answer must refuse, and the tool ran %d times", tool.calls)
	}
	refusal := row.Steps[0].ToolCalls[0].Error
	if !strings.Contains(refusal, "no person was available to answer") {
		t.Fatalf("the refusal does not say a person was not there: %q", refusal)
	}
	t.Logf("the model reads: %s", toolResultSeenByTheModel(t, model))
}

func TestUnderEnforceAnAskFollowsThePersonsAnswer(t *testing.T) {
	for _, answer := range []PersonAnswer{PersonAllowedOnce, PersonAlwaysHere, PersonDenied} {
		tool := &stubTool{name: "write", result: Result{Content: "ok"}}
		model := callThenAnswer(judgedWrite("c1", "a.txt"))
		var asked GateRequest
		config := enforced(gateSaying(ledger.VerdictAsk), tool, model)
		config.Person = func(_ context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
			asked = request
			if decision.Verdict != ledger.VerdictAsk {
				t.Fatalf("the person was asked about a %s verdict", decision.Verdict)
			}
			return answer, nil
		}

		row, err := Run(context.Background(), config)
		if err != nil {
			t.Fatalf("Run returned an error: %v", err)
		}
		if asked.Tool != "write" || string(asked.Args) != `{"path":"a.txt"}` {
			t.Fatalf("the person was shown %+v, want the call itself", asked)
		}
		ran := 0
		if answer != PersonDenied {
			ran = 1
		}
		if tool.calls != ran {
			t.Fatalf("the person answered %d and the tool ran %d times, want %d", answer, tool.calls, ran)
		}
		refusal := row.Steps[0].ToolCalls[0].Error
		if ran == 1 && refusal != "" {
			t.Fatalf("the person allowed the call and it was still refused: %q", refusal)
		}
		if ran == 0 && !strings.Contains(refusal, "the person did not allow it") {
			t.Fatalf("the person refused the call and the model is told %q", refusal)
		}
		t.Logf("answered %d: tool ran %d times, refusal %q", answer, tool.calls, refusal)
	}
}
