package host

import (
	"context"
	"encoding/json"
	"testing"

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
