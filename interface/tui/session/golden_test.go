package session

import (
	"testing"
	"time"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/markdown"
	"tofu/internal/golden"
)

func finishedTurn(at *time.Time) *Model {
	model := New(func() time.Time { return *at }, new(markdown.Renderer).Lines)
	model.SetSize(120, 36)
	model.Focus()
	model.Append(Entry{Kind: User, Body: "why does the gate read the policy first?"})
	model.Start()
	*at = at.Add(2 * time.Second)
	model.Returned()
	for step, call := range []Entry{
		{Kind: Tool, ID: "toolu_c0ffee01", Head: "read", Body: "internal/judge/policy/policy.go"},
		{Kind: Tool, ID: "toolu_c0ffee02", Head: "grep", Body: "Resolve internal/judge"},
		{Kind: Tool, ID: "toolu_c0ffee03", Head: "bash", Body: "go test ./internal/judge/..."},
	} {
		*at = at.Add(3 * time.Second)
		model.Append(call)
		model.Finish(call.ID, Result{Status: "ok " + time.Duration(step+1).String()})
	}
	*at = at.Add(4 * time.Second)
	model.Append(Entry{Kind: Assistant, ID: "msg_a1b2c3", Body: "The gate reads the **policy** first because a locked policy decides the mode.\n\n- `policy.Resolve` turns a declared mode into a resolved one\n- the wire is asked only after that\n\nSo an unlocked policy never reaches `jev`."})
	model.Close("cooked for", "toolu_c0ffee03")
	model.Stop()
	return &model
}

func TestSessionViewGolden(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 31, 0, 0, time.UTC)
	model := finishedTurn(&at)
	golden.Assert(t, "session-120x36.golden", model.View())
}

func TestSessionAwaitingGolden(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 31, 0, 0, time.UTC)
	model := finishedTurn(&at)
	at = at.Add(time.Minute)
	model.Append(Entry{Kind: User, Body: "push the fix to the branch"})
	model.Start()
	at = at.Add(time.Second)
	model.Returned()
	at = at.Add(2 * time.Second)
	model.Append(Entry{Kind: Tool, ID: "toolu_d00d11", Head: "bash", Body: "git push --force origin develop"})
	model.Decide(Decision{
		Tool:    "bash",
		Verdict: Ask,
		Answers: []Answer{
			{Question: "approval", Value: 0.75, Max: 1},
			{Question: "risk", Value: 2, Max: 3},
		},
		Reason: Reason{Question: "risk", Limit: "risk_ask_at", Levels: fixture.RiskLevels(), Threshold: 1.5, Value: 2},
	})
	model.Await()
	at = at.Add(21 * time.Second)
	golden.Assert(t, "session-awaiting-120x36.golden", model.View())
}
