package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

func settingsAsk(value string) turn.GateRequest {
	return turn.GateRequest{Tool: turn.SettingsToolName, Args: json.RawMessage(`{"key":"subAgentsPerTurn","value":` + value + `}`)}
}

func TestAnApprovalNamesItsAnswersRefusesAWrongOneAndAStandingRefusalIsARow(t *testing.T) {
	var h *Host
	answers := make(chan turn.PersonAnswer, 8)
	play := func(ctx context.Context, _ Pick, _ string, live Live) {
		person := awaitPerson(live.Emit, h.asks)
		for _, request := range []turn.GateRequest{settingsAsk("3"), {Tool: hookTrustTool, Args: json.RawMessage(`{"command":"PreToolUse *: make lint"}`)}, settingsAsk("9"), settingsAsk("9")} {
			answer, _ := person(ctx, request, turn.GateDecision{Verdict: ledger.VerdictAsk, PersonOnly: true})
			answers <- answer
		}
	}
	c, live, _ := serving(t, play, ServeConfig{})
	h = live
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"change a setting"}`)

	type request struct {
		Approval  string   `json:"approval"`
		Target    string   `json:"target"`
		Decisions []string `json:"decisions"`
	}
	requested := func() request {
		var asked request
		c.until(func(line wireLine) bool {
			return line.Method == ApprovalMethod && json.Unmarshal(line.Params, &asked) == nil
		}, "an approval request")
		return asked
	}
	reply := func(id, body string) {
		if _, err := io.WriteString(c.in, `{"jsonrpc":"2.0","id":"`+id+`",`+body+"}\n"); err != nil {
			t.Fatal(err)
		}
	}
	refusedOrState := func(id string) wireLine {
		c.ask("probe-"+id, "session.state", `{}`)
		return c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` || string(line.ID) == `"probe-`+id+`"` }, "a reply to "+id)
	}

	first := requested()
	if want := []string{"allow_once", "allow_always", "reject_once", "reject_always", "cancelled"}; !slices.Equal(first.Decisions, want) {
		t.Errorf("a settings approval lists %v, want %v", first.Decisions, want)
	}
	reply("no-such-approval", `"result":{"decision":"allow_once"}`)
	if line := refusedOrState("no-such-approval"); line.Error == nil {
		t.Errorf("an answer to an approval nobody asked got %s rather than an error reply", line.ID)
	}
	reply(first.Approval, `"result":{"decision":"remember_global"}`)
	if line := refusedOrState(first.Approval); line.Error == nil {
		t.Errorf("remember_global on a settings approval got %s rather than an error naming what it takes", line.ID)
	}
	reply("a-client-error", `"error":{"code":-32000,"message":"the client failed"}`)
	if line := refusedOrState("a-client-error"); string(line.ID) == `"a-client-error"` {
		t.Errorf("an error the client sent was answered with another, so two programs can trade errors forever: %+v", line.Error)
	}
	reply(first.Approval, `"result":{"decision":"allow_always"}`)
	var resolved struct {
		Decision string `json:"decision"`
		Standing bool   `json:"standing"`
	}
	c.until(func(line wireLine) bool {
		return line.Method == "approval.resolved" && json.Unmarshal(line.Params, &resolved) == nil
	}, "approval.resolved")
	if resolved.Decision != "allow_always" || !resolved.Standing {
		t.Errorf("approval.resolved = %+v, want allow_always and standing", resolved)
	}
	trust := requested()
	c.ask("4", "session.state", `{}`)
	var state struct {
		Standing []struct{ Target, Decision string } `json:"standing"`
	}
	c.answer("4", &state)
	if len(state.Standing) != 1 || state.Standing[0].Target != first.Target || state.Standing[0].Decision != "allow_always" {
		t.Errorf("session.state standing = %+v, want %q always", state.Standing, first.Target)
	}
	reply(first.Approval, `"result":{"decision":"allow_once"}`)
	if line := refusedOrState(first.Approval); line.Error == nil {
		t.Errorf("a second answer to a resolved approval got %s rather than an error reply", line.ID)
	}

	if want := []string{"allow_once", "allow_always", "reject_once", "cancelled"}; !slices.Equal(trust.Decisions, want) {
		t.Errorf("a hooks trust ask lists %v, want %v: it never stands, so never here means nothing there", trust.Decisions, want)
	}
	reply(trust.Approval, `"result":{"decision":"allow_always"}`)
	never := requested()
	reply(never.Approval, `"result":{"decision":"reject_always"}`)
	got := []turn.PersonAnswer{<-answers, <-answers, <-answers, <-answers}
	if want := []turn.PersonAnswer{turn.PersonAlwaysHere, turn.PersonAlwaysHere, turn.PersonDenied, turn.PersonDenied}; !slices.Equal(got, want) {
		t.Fatalf("the four asks answered %v, want %v", got, want)
	}
	c.ask("5", "session.state", `{}`)
	c.answer("5", &state)
	if len(state.Standing) != 2 {
		t.Errorf("session.state standing after a trust always and a never here = %+v, want the settings always and the never here alone", state.Standing)
	}
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	var rows []ledger.Row
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{Verdict: ledger.VerdictDeny}, func(row ledger.Row) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var refused struct{ Target string }
	if len(rows) != 1 || json.Unmarshal(rows[0].State, &refused) != nil || refused.Target != never.Target || rows[0].Point != standingPoint {
		t.Errorf("the ledger holds %d deny rows %+v, want one for the call never here refused unasked", len(rows), rows)
	}
}

func TestStoppingTheLeadResolvesItsWaitingApprovalCancelled(t *testing.T) {
	var h *Host
	answered := make(chan error, 1)
	play := func(ctx context.Context, _ Pick, _ string, live Live) {
		_, err := h.inbox.LeadAsks(awaitPerson(live.Emit, h.asks))(ctx, settingsAsk("4"), turn.GateDecision{Verdict: ledger.VerdictAsk, PersonOnly: true})
		answered <- err
	}
	c, live, _ := serving(t, play, ServeConfig{})
	h = live
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"change a setting"}`)
	c.until(func(line wireLine) bool { return line.Method == ApprovalMethod }, "the approval request")
	c.ask("4", "turn.stop", `{"lead":true}`)
	var resolved struct{ Decision, By string }
	c.until(func(line wireLine) bool {
		return line.Method == "approval.resolved" && json.Unmarshal(line.Params, &resolved) == nil
	}, "approval.resolved after the lead stopped")
	if resolved.Decision != "cancelled" {
		t.Errorf("the approval resolved %+v, want cancelled", resolved)
	}
	select {
	case err := <-answered:
		if err == nil {
			t.Error("the withdrawn ask returned no error, so the call reads as answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the ask still waits after the lead stopped")
	}
}

type caughtHooks chan Hooks

func (c caughtHooks) Prepare(_ Turn, hooks Hooks) (Prepared, error) {
	c <- hooks
	return Prepared{}, errors.New("prepared nothing")
}

func (caughtHooks) Renew() {}

func (caughtHooks) OneTurnPerProject() bool { return false }

func TestACronTurnRefusesWhatWouldAskAndNothingWaits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	caught := make(caughtHooks, 1)
	h, _ := New(Config{Dir: t.TempDir(), Engine: caught})
	t.Cleanup(h.Close)
	for _, origin := range []Origin{{Kind: OriginCron, Job: "c1"}, {Kind: OriginPerson}} {
		h.mu.Lock()
		h.begin(Pick{}, "check the build", origin)
		h.mu.Unlock()
		hooks := <-caught
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		answer, err := hooks.Person(ctx, settingsAsk("5"), turn.GateDecision{Verdict: ledger.VerdictAsk, PersonOnly: true})
		cancel()
		waited := errors.Is(err, context.DeadlineExceeded)
		switch origin.Kind {
		case OriginCron:
			if waited || answer != turn.PersonDenied || err == nil || !strings.Contains(err.Error(), "no person") {
				t.Errorf("a cron turn's ask answered %v, %v, want refused at once with no person", answer, err)
			}
		default:
			if !waited {
				t.Errorf("a person's turn did not wait for the person: %v, %v", answer, err)
			}
		}
		for _, running := h.Turn(); running; _, running = h.Turn() {
			time.Sleep(time.Millisecond)
		}
	}
}
