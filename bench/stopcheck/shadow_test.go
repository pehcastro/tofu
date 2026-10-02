package stopcheck

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/turn"
)

type queuedModel struct {
	decisions []llm.Decision
	asked     int
}

func (m *queuedModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if m.asked >= len(m.decisions) {
		return llm.Decision{}, errors.New("queuedModel: the loop asked for more steps than were queued")
	}
	m.asked++
	return m.decisions[m.asked-1], nil
}

type echoTool struct {
	calls int
}

func (*echoTool) Name() string { return "bash" }

func (*echoTool) Definition() llm.Tool {
	return llm.Tool{Name: "bash", Description: "run a command", Parameters: map[string]any{"type": "object"}}
}

func (t *echoTool) Run(context.Context, json.RawMessage) (turn.Result, error) {
	t.calls++
	return turn.Result{Content: "ok " + strconv.Itoa(t.calls), Command: "ls"}, nil
}

type shadowGate struct {
	deciding sync.Mutex
	writer   *ledger.Writer
	pol      gate.Rule
	mode     gate.Mode
	reason   string
	written  []ledger.Row
}

func (g *shadowGate) Decide(_ context.Context, request turn.GateRequest) (turn.GateDecision, error) {
	g.deciding.Lock()
	defer g.deciding.Unlock()
	built, builder, err := state.BuildStopCheck(state.StopCheckState{
		Task:        request.Task,
		RecentSteps: []state.StopCheckStep{{Index: len(g.written) + 1, ToolCalls: []state.StopCheckCall{{Tool: request.Tool}}}},
	})
	if err != nil {
		return turn.GateDecision{}, err
	}
	row, err := appendRow(g.writer, rowInput{
		state: json.RawMessage(built), stateBuilder: builder, wording: g.pol.QuestionsVersion,
		answers: stopNowAnswers(g.pol.QuestionsVersion), pol: g.pol, mode: g.mode, modeReason: g.reason,
		turnID: request.TurnID,
	})
	if err != nil {
		return turn.GateDecision{}, err
	}
	g.written = append(g.written, row)
	return turn.GateDecision{ID: row.ID, Verdict: ledger.VerdictAllow}, nil
}

func stopNowAnswers(wording int) []ledger.Answer {
	return []ledger.Answer{
		{Question: "budget_exhausted", Wording: wording, Kind: ledger.AnswerNoul, Noul: 0.02},
		{Question: "stalled", Wording: wording, Kind: ledger.AnswerNoul, Noul: 0.93},
		{Question: "stop_pressure", Wording: wording, Kind: ledger.AnswerScore, Score: 3},
		{Question: "work_remains", Wording: wording, Kind: ledger.AnswerNoul, Noul: 0.03},
	}
}

func shippedRule(t *testing.T) (gate.Rule, gate.Resolution) {
	t.Helper()
	pol, _, err := state.StopCheckRule()
	if err != nil {
		t.Fatalf("loading the shipped stop_check rule: %v", err)
	}
	return pol, gate.Resolve(pol, gate.LockLookup{}, gate.Current{})
}

func TestTheShippedRuleResolvesToShadow(t *testing.T) {
	pol, resolution := shippedRule(t)
	if !pol.ModeDeclared || pol.Mode != gate.ModeShadow {
		t.Fatalf("the shipped rule declares mode %q, want shadow", pol.Mode)
	}
	if resolution.Mode != gate.ModeShadow {
		t.Fatalf("gate.Resolve made stop_check %s: %s", resolution.Mode, resolution.Reason)
	}
	t.Logf("stop_check resolves to %s: %s", resolution.Mode, resolution.Reason)
}

func TestAStopDecisionRecordedMidTurnInShadowDoesNotEndTheTurn(t *testing.T) {
	pol, resolution := shippedRule(t)
	dir := t.TempDir()
	gate := &shadowGate{writer: ledger.NewWriter(dir), pol: pol, mode: resolution.Mode, reason: resolution.Reason}

	call := llm.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}},
		{Build: "stub", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}},
		{Build: "stub", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}},
		{Build: "stub", Outcome: llm.OutcomeMessage, Content: "done"},
	}}

	row, err := turn.Run(context.Background(), turn.Config{
		Model:          model,
		Spend:          turn.SpendAPIKey,
		Tools:          turn.NewRegistry(&echoTool{}),
		Gate:           gate,
		Task:           "list the folder",
		Caps:           turn.Caps{MaxSteps: 10, LoopGuardRepeats: konst.TurnLoopGuardRepeats, LoopGuardWindow: konst.TurnLoopGuardWindow},
		ResultBytesCap: 4096,
	})
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}

	if len(gate.written) != 3 {
		t.Fatalf("the shadow point recorded %d decisions, want one per tool call", len(gate.written))
	}
	for _, written := range gate.written {
		if written.Verdict != ledger.VerdictDeny {
			t.Fatalf("row %s recorded verdict %s, want deny, which for stop_check reads stop the turn", written.ID, written.Verdict)
		}
		if written.Reason.Mode != ledger.ModeShadow {
			t.Fatalf("row %s recorded mode %s, want shadow", written.ID, written.Reason.Mode)
		}
		reread, present, err := ledger.NewReader(dir).ByID(written.ID)
		if err != nil || !present {
			t.Fatalf("row %s is not readable back out of the ledger: present %v err %v", written.ID, present, err)
		}
		if reread.Verdict != ledger.VerdictDeny || reread.Point != "stop_check" {
			t.Fatalf("row %s came back as point %q verdict %s", reread.ID, reread.Point, reread.Verdict)
		}
	}

	if len(row.Steps) != 4 {
		t.Fatalf("the turn ran %d steps after three recorded stop decisions, want the four its model asked for", len(row.Steps))
	}
	if row.Outcome != turn.OutcomeStopped {
		t.Fatalf("the turn ended as %s, want stopped, which is the model's own last message ending it", row.Outcome)
	}
	if model.asked != 4 {
		t.Fatalf("the loop asked the model %d times, want 4", model.asked)
	}
	t.Logf("three deny rows in shadow, and the turn still ran %d steps and ended %s", len(row.Steps), row.Outcome)
}
