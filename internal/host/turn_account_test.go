package host

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
)

type scriptedModel struct{ replies []llm.Decision }

func (m *scriptedModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if len(m.replies) == 0 {
		return llm.Decision{Build: "stub", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return reply, nil
}

type twoAccounts struct {
	dir   string
	fixed bool
}

func (e twoAccounts) Prepare(start Turn, hooks Hooks) (Prepared, error) {
	first := &scriptedModel{replies: []llm.Decision{{Build: "stub", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "nothing", Arguments: json.RawMessage(`{}`)}}}}}
	moved := false
	accounts := turn.Accounts{
		Pick: func(context.Context) (turn.Account, error) {
			if e.fixed {
				return turn.Account{Model: first}, nil
			}
			return turn.Account{ID: 1, Model: first, Headroom: 0.2, Window: "5h"}, nil
		},
		Next: func(context.Context, turn.Account) (turn.Account, bool, error) {
			if moved || e.fixed {
				return turn.Account{}, false, nil
			}
			moved = true
			return turn.Account{ID: 2, Model: &scriptedModel{}, Headroom: 0.5, Window: "5h"}, true, nil
		},
	}
	store, err := session.OpenIn(e.dir)
	return Prepared{Config: turn.Config{Accounts: accounts, Task: start.Task, Caps: turn.Caps{MaxSteps: 4}, Inbox: hooks.Inbox, ResultBytesCap: konst.TurnResultBytesCap, Spend: turn.SpendSubscription}, Sessions: store,
		Account: func(id int64) TurnAccount {
			return TurnAccount{Source: "claude-sub", AccountID: id, Login: "person@example.com", Model: "claude-opus-5"}
		}}, err
}

func (twoAccounts) Renew() {}

func (twoAccounts) OneTurnPerProject() bool { return false }

func TestQuotaIsReadAgainWhenTheTurnMovesToAnotherAccount(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{Dir: project, Engine: twoAccounts{dir: project}})
	t.Cleanup(h.Close)
	reads := make(chan struct{}, 8)
	c := servingHost(t, h, project, ServeConfig{Quota: func() []QuotaWindow {
		reads <- struct{}{}
		return nil
	}})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.answer("1", &InitializeResult{})
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"read the note"}`)
	c.answer("3", &TurnResult{})
	c.until(func(line wireLine) bool { return line.Method == "turn.completed" }, "turn.completed")
	for read := range 3 {
		select {
		case <-reads:
		case <-time.After(5 * time.Second):
			t.Fatalf("the quota was read %d times, want 3: at session.open, at the move to account 2, and at the end of the turn", read)
		}
	}
}

func TestAccountStateFollowsEachAccountAndAPassedRetryReadsTheQuotaAgain(t *testing.T) {
	readAt := time.Date(2026, 10, 9, 5, 17, 12, 0, time.UTC)
	retry := time.Now().Add(400 * time.Millisecond)
	rounds := [][]AccountNow{
		{{Source: "claude-sub", AccountID: 1, State: ConditionRateLimited, RetryAt: retry}, {Source: "codex-sub", AccountID: 2, State: ConditionServing}},
		{{Source: "claude-sub", AccountID: 1, State: ConditionSpent}, {Source: "codex-sub", AccountID: 2, State: ConditionServing}},
		{{Source: "claude-sub", AccountID: 1, State: ConditionServing}, {Source: "codex-sub", AccountID: 2, State: ConditionServing}},
	}
	var polls atomic.Int32
	c, _, _ := serving(t, nil, ServeConfig{
		Quota: func() []QuotaWindow {
			stale := polls.Add(1) == 1
			return []QuotaWindow{{Account: "#1", Window: "claude-sub 5h", Percent: 34, Reported: true, Source: "claude-sub", AccountID: 1, Stale: stale, ReadAt: readAt}}
		},
		Accounts: func() []AccountNow { return rounds[min(int(polls.Load()), len(rounds))-1] },
	})
	var states []AccountStateChanged
	var windows []QuotaWindow
	until := func(state AccountCondition) {
		c.until(func(line wireLine) bool {
			switch line.Method {
			case "quota.updated":
				var updated QuotaUpdated
				_ = json.Unmarshal(line.Params, &updated)
				windows = append(windows, updated.Windows...)
			case "account.state":
				var changed AccountStateChanged
				_ = json.Unmarshal(line.Params, &changed)
				states = append(states, changed)
				return changed.State == state
			}
			return false
		}, "account.state "+string(state))
	}
	c.ask("1", "initialize", `{"client":"desk"}`)
	c.answer("1", &InitializeResult{})
	c.ask("2", "session.open", `{}`)
	c.answer("2", &SessionOpenResult{})
	until(ConditionRateLimited)
	until(ConditionSpent)
	c.ask("3", "session.open", `{}`)
	c.answer("3", &SessionOpenResult{})
	until(ConditionServing)

	if len(states) != 3 || states[0].Source != "claude-sub" || states[0].AccountID != 1 || !states[0].RetryAt.Equal(retry) || states[1].State != ConditionSpent {
		t.Errorf("account.state went out as %+v, want claude-sub 1 rate_limited until %s, then spent, then serving, and nothing for codex-sub, which never changed", states, retry)
	}
	if len(windows) < 2 || !windows[0].Stale || !windows[0].ReadAt.Equal(readAt) || windows[1].Stale {
		t.Errorf("quota.updated carried %+v, want the first window stale and read at %s, then a fresh one", windows, readAt)
	}
}

func TestTurnAccountNamesThePickAndTheMoveOfTheRunningTurn(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		home, project := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		h, _ := New(Config{Dir: project, Engine: twoAccounts{dir: project, fixed: fixed}})
		t.Cleanup(h.Close)
		c := servingHost(t, h, project, ServeConfig{})
		c.ask("1", "initialize", `{"client":"scratch"}`)
		c.ask("2", "session.open", `{}`)
		var opened SessionOpenResult
		c.answer("2", &opened)
		c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"read the note"}`)
		var running TurnResult
		c.answer("3", &running)
		var spent []TurnAccount
		c.until(func(line wireLine) bool {
			if line.Method == "turn.account" {
				var one TurnAccount
				_ = json.Unmarshal(line.Params, &one)
				spent = append(spent, one)
				t.Logf("turn.account %s", line.Params)
			}
			return line.Method == "turn.completed"
		}, "turn.completed")
		if fixed {
			if len(spent) != 0 {
				t.Errorf("a fixed model with no account sent %+v", spent)
			}
			continue
		}
		if len(spent) != 2 {
			t.Fatalf("the turn sent %d turn.account lines, want a pick and a move: %+v", len(spent), spent)
		}
		picked, moved := spent[0], spent[1]
		if picked.Reason != AccountReason(turn.AccountPicked) || picked.AccountID != 1 || picked.FromAccount != 0 || picked.Source != "claude-sub" || picked.Login != "person@example.com" || picked.Model != "claude-opus-5" || picked.Turn != running.Turn {
			t.Errorf("the pick went out as %+v, want account 1 picked on claude-sub in turn %s", picked, running.Turn)
		}
		if moved.Reason != AccountReason(turn.AccountMoved) || moved.AccountID != 2 || moved.FromAccount != 1 || moved.Turn != running.Turn {
			t.Errorf("the move went out as %+v, want account 2 moved from 1 in turn %s", moved, running.Turn)
		}
	}
}
