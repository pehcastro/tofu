package stopcheck

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"

	"tofu/internal/agent/loop"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
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

type handback struct {
	turn Turn
	done bool
}

func ran(tool, command string) Call { return Call{Tool: tool, Command: command} }

func failed(tool, command string) Call { return Call{Tool: tool, Command: command, Failed: true} }

func mockedHandbacks() []handback {
	closings := []string{"Done.", "Finished, the change is in.", "The task is complete.", "That is handled now."}
	tasks := []string{
		"create hello.txt containing the word hello",
		"fix the failing test in parse_test.go",
		"rename the function load to loadConfig in config.go and its callers",
		"add a --verbose flag to cmd/tool/main.go",
		"delete the unused file old_helpers.go",
		"write notes.md and todo.md, one line each",
		"set the version in package.json to 1.2.0",
		"make npm run lint pass in src/",
		"add a test for the empty input case in split.go",
		"change the port from 8080 to 9090 in server.yaml",
		"format main.go with gofmt",
		"count the lines in data.csv and write the number to count.txt",
	}
	did := [][]Call{
		{ran("write", "write hello.txt")},
		{ran("read", "read parse.go"), ran("edit", "edit parse.go"), ran("bash", "go test ./parse")},
		{ran("bash", "grep -rn load( ."), ran("edit", "edit config.go"), ran("edit", "edit main.go"), ran("bash", "go build ./...")},
		{ran("read", "read cmd/tool/main.go"), ran("edit", "edit cmd/tool/main.go"), ran("bash", "go build ./cmd/tool"), ran("bash", "./tool --verbose")},
		{ran("bash", "rm old_helpers.go"), ran("bash", "go build ./...")},
		{ran("write", "write notes.md"), ran("write", "write todo.md")},
		{ran("edit", "edit package.json"), ran("bash", "cat package.json")},
		{failed("bash", "npm run lint"), ran("edit", "edit src/a.ts"), ran("bash", "npm run lint")},
		{ran("write", "write split_test.go"), ran("bash", "go test ./split")},
		{ran("edit", "edit server.yaml"), ran("bash", "grep 9090 server.yaml")},
		{ran("bash", "gofmt -w main.go"), ran("bash", "gofmt -l main.go")},
		{ran("bash", "wc -l data.csv"), ran("write", "write count.txt")},
	}
	left := [][]Call{
		nil,
		{ran("read", "read parse.go"), ran("edit", "edit parse.go"), failed("bash", "go test ./parse")},
		{ran("edit", "edit config.go"), failed("bash", "go build ./...")},
		{ran("read", "read cmd/tool/main.go")},
		{failed("bash", "rm old_helpers.go")},
		{ran("write", "write notes.md")},
		{failed("edit", "edit package.json")},
		{failed("bash", "npm run lint"), ran("edit", "edit src/a.ts"), failed("bash", "npm run lint")},
		{ran("write", "write split_test.go"), failed("bash", "go test ./split")},
		{ran("read", "read server.yaml")},
		{failed("bash", "gofmt -w main.go")},
		{ran("bash", "wc -l data.csv")},
	}
	var cases []handback
	for i, task := range tasks {
		for _, c := range []struct {
			calls []Call
			done  bool
		}{{did[i], true}, {left[i], false}} {
			turn := Turn{ID: "mock-" + strconv.Itoa(len(cases)+1), Task: task}
			for _, call := range c.calls {
				turn.Steps = append(turn.Steps, Step{Index: len(turn.Steps) + 1, Calls: []Call{call}})
			}
			turn.Steps = append(turn.Steps, Step{Index: len(turn.Steps) + 1, AssistantText: closings[i%len(closings)]})
			cases = append(cases, handback{turn: turn, done: c.done})
		}
	}
	return cases
}

func TestLiveStopCheckOnTheMockedHandbacksAgainstTheArmThatNeverStops(t *testing.T) {
	battery, _, _ := liveBattery(t)
	cases := mockedHandbacks()
	neverStopRight := 0
	for _, c := range cases {
		if !c.done {
			neverStopRight++
		}
	}
	for pass := 1; pass <= 2; pass++ {
		var right, falseStops, falseContinues int
		var spent float64
		var latencies []int64
		for _, c := range cases {
			decision := askDirect(t, battery, StateAt(c.turn, len(c.turn.Steps)-1))
			verdict, _, err := gate.Decide(decision.Answers, battery.pol)
			if err != nil {
				t.Fatalf("%s: %v", c.turn.ID, err)
			}
			stopped := typedAnswer(verdict.Ledger()) == Stop
			switch {
			case stopped == c.done:
				right++
			case stopped:
				falseStops++
			default:
				falseContinues++
			}
			spent += decision.Charge.USD
			latencies = append(latencies, decision.Latency.Milliseconds())
			t.Logf("pass %d %s done=%v jev=%s work_remains=%.2f build=%s", pass, c.turn.ID, c.done, verdict.Ledger(), decision.Answers["work_remains"].Noul, decision.Build)
		}
		slices.Sort(latencies)
		t.Logf("pass %d on %s: jev %d of %d right, %d false stops, %d false continues; never stops %d of %d; p50 %d ms, worst %d ms, $%.6f",
			pass, battery.model, right, len(cases), falseStops, falseContinues, neverStopRight, len(cases), latencies[len(latencies)/2], latencies[len(latencies)-1], spent)
	}
}

func TestTheMockedHandbackCorpusDoesNotNameItsLabel(t *testing.T) {
	cases := mockedHandbacks()
	if len(cases) < 20 {
		t.Fatalf("the mocked corpus holds %d hand-backs, want at least 20", len(cases))
	}
	said := map[string]map[bool]int{}
	for _, c := range cases {
		last := c.turn.Steps[len(c.turn.Steps)-1].AssistantText
		if said[last] == nil {
			said[last] = map[bool]int{}
		}
		said[last][c.done]++
	}
	for text, labels := range said {
		if labels[true] == 0 || labels[false] == 0 {
			t.Errorf("the closing line %q is said only by hand-backs labelled done=%v, so it names its own answer", text, labels[true] > 0)
		}
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
