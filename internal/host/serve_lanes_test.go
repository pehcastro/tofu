package host

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"tofu/internal/status"
)

func TestServeKeepsASessionRunningWhileAnotherOpens(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var spawned atomic.Int32
	newHost := func() (*Host, func()) {
		var h *Host
		h, _ = New(Config{Dir: project, Play: func(ctx context.Context, _ Pick, task string, live Live) {
			if !strings.Contains(task, "long") {
				live.Emit(Event{Kind: EventText, Text: "a short answer"})
				return
			}
			reply, forget := h.asks.wait("ask-"+live.Turn, "")
			defer forget()
			live.Emit(Event{Kind: EventAwaitPerson, ID: "ask-" + live.Turn, Tool: "bash", Text: "make build", Args: json.RawMessage(`{"command":"make build"}`), Accepts: []ApprovalDecision{AllowOnce}})
			select {
			case <-reply:
			case <-ctx.Done():
			}
		}})
		spawned.Add(1)
		return h, h.Close
	}
	first, closeFirst := newHost()
	t.Cleanup(closeFirst)
	spawned.Store(0)
	c := servingHost(t, first, project, ServeConfig{Spawn: newHost})
	var seen []wireLine
	keep := func(wanted func(wireLine) bool) func(wireLine) bool {
		return func(line wireLine) bool {
			seen = append(seen, line)
			return wanted(line)
		}
	}
	about := func(line wireLine) Identity {
		var id Identity
		_ = json.Unmarshal(line.Params, &id)
		return id
	}
	refused := func(id, method, params string) {
		t.Helper()
		c.ask(id, method, params)
		if line := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id); line.Error == nil {
			t.Errorf("%s %s was accepted", method, params)
		}
	}

	c.ask("1", "initialize", `{"client":"desk"}`)
	c.answer("1", &InitializeResult{})
	var idle, a, b SessionOpenResult
	c.ask("2", "session.open", `{}`)
	c.answer("2", &idle)
	c.ask("3", "session.open", `{}`)
	c.answer("3", &a)
	if spawned.Load() != 0 || a.Session == idle.Session {
		t.Fatalf("a second session.open while the first was idle spawned %d hosts and opened %s after %s, want the one host reused for a new session", spawned.Load(), a.Session, idle.Session)
	}

	c.ask("4", "turn.send", `{"session":"`+a.Session+`","text":"a long job"}`)
	var aTurn TurnResult
	c.answer("4", &aTurn)
	asked := c.until(keep(func(line wireLine) bool { return line.Method == ApprovalMethod }), "A's approval request")
	if about(asked).Session != a.Session {
		t.Errorf("A's approval request names session %q, want %s", about(asked).Session, a.Session)
	}

	c.ask("5", "session.open", `{}`)
	c.answer("5", &b)
	if spawned.Load() != 1 || b.Session == a.Session {
		t.Fatalf("session.open while A ran spawned %d hosts and opened %s, want one new host for a new session", spawned.Load(), b.Session)
	}
	c.ask("6", "turn.send", `{"session":"`+b.Session+`","text":"a short one"}`)
	var bTurn TurnResult
	c.answer("6", &bTurn)
	c.until(keep(func(line wireLine) bool { return line.Method == "turn.completed" && about(line).Turn == bTurn.Turn }), "B's turn.completed")

	var focus SessionState
	c.ask("7", "session.state", `{}`)
	c.answer("7", &focus)
	if focus.Session != b.Session || focus.Running {
		t.Errorf("session.state without a session answers %s running %v, want B, the most recently opened, idle", focus.Session, focus.Running)
	}

	var all StatusList
	c.ask("8", "status.list", `{}`)
	c.answer("8", &all)
	leads := map[string]status.State{}
	for _, record := range all.Records {
		if record.ID == statusApp {
			leads[record.Session] = record.State
		}
	}
	if leads[a.Session] != status.Blocked || leads[b.Session] != status.Done {
		t.Errorf("status.list {} leads by session = %v, want A blocked and B done", leads)
	}
	var onlyA StatusList
	c.ask("9", "status.list", `{"session":"`+a.Session+`"}`)
	c.answer("9", &onlyA)
	for _, record := range onlyA.Records {
		if record.Session != a.Session {
			t.Errorf("status.list naming A lists %+v", record)
		}
	}
	if len(onlyA.Records) == 0 {
		t.Error("status.list naming A, a background session, lists nothing")
	}

	if _, err := io.WriteString(c.in, `{"jsonrpc":"2.0","id":"ask-`+aTurn.Turn+`","result":{"decision":"allow_once"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	c.until(keep(func(line wireLine) bool { return line.Method == "turn.completed" && about(line).Turn == aTurn.Turn }), "A's turn.completed after its approval")
	for _, line := range seen {
		if id := about(line); id.Turn == aTurn.Turn && id.Session != a.Session || id.Turn == bTurn.Turn && id.Session != b.Session {
			t.Errorf("%s of turn %s names session %s", line.Method, id.Turn, id.Session)
		}
	}

	c.ask("10", "turn.send", `{"session":"`+a.Session+`","text":"a long job again"}`)
	var again TurnResult
	c.answer("10", &again)
	if again := c.until(func(line wireLine) bool { return line.Method == ApprovalMethod }, "A's second approval request"); about(again).Session != a.Session {
		t.Errorf("turn.send naming A, out of focus, asked under session %s", about(again).Session)
	}
	refused("11", "session.close", `{"session":"`+a.Session+`"}`)
	c.ask("12", "session.close", `{"session":"`+a.Session+`","stop":true}`)
	c.until(func(line wireLine) bool {
		return line.Method == statusMethod && about(line).Session == a.Session && strings.Contains(string(line.Params), `"state":"clear"`)
	}, "a clear status record for the closed session A")
	c.answer("12", &Ack{})
	var left StatusList
	c.ask("13", "status.list", `{}`)
	c.answer("13", &left)
	for _, record := range left.Records {
		if record.Session != b.Session {
			t.Errorf("after session.close of A, status.list still lists %+v", record)
		}
	}
	refused("14", "turn.send", `{"session":"`+a.Session+`","text":"after the close"}`)
	refused("15", "session.close", `{"session":"`+b.Session+`"}`)
}
