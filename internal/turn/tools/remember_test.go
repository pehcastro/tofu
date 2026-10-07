package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tofu/internal/memory"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const (
	olderSession = "turn-18d7474e7fae0001"
	newerSession = "turn-18d7474e7fae0002"
)

func said(t *testing.T, id, origin, text string) session.Event {
	t.Helper()
	body, err := json.Marshal(session.MessageBody{Role: session.RoleUser, Content: text, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return session.Event{ID: id, Attempt: session.FirstAttempt, Kind: session.EventMessage, Body: body}
}

func rememberChain(t *testing.T, answer turn.PersonAnswer) (tools.Remember, *int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	store := session.NewStore(t.TempDir())
	if err := store.Write(session.Header{ID: olderSession, Root: olderSession, At: time.Now()}, []session.Event{
		said(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2201", "task", "<env>\ntoday\n</env>"+turn.TheTaskFollows+"never run cargo with more than 2 jobs, ok?"+turn.NotesAfterTheTask+"[code_rules, from the rule e2e_first]\nprove it end to end"),
		said(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2202", "sub-agent report", "the agent said the build is make all"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(session.Header{ID: newerSession, Root: olderSession, CarriedFrom: &session.Carried{Session: olderSession}, At: time.Now()}, []session.Event{
		said(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2203", "typed by the person", "and   keep replies\nshort"),
		said(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2204", "typed by the person", "ok. also remember that the logs go to /tmp"),
	}); err != nil {
		t.Fatal(err)
	}
	asked := 0
	ask := func(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
		asked++
		return answer, nil
	}
	return tools.Remember{Ask: ask, Store: store, Session: newerSession, Project: t.TempDir(), Inbox: turn.NewInbox(), Auto: func() bool { return false }}, &asked
}

func remembered(t *testing.T, tool tools.Remember, statement, quote, kind string) (turn.Result, error) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"statement": statement, "said": quote, "kind": kind})
	if err != nil {
		t.Fatal(err)
	}
	return tool.Run(context.Background(), args)
}

func kept(t *testing.T, tool tools.Remember) memory.Memory {
	t.Helper()
	shelves, err := memory.Open(tool.Project)
	if err != nil {
		t.Fatal(err)
	}
	return shelves
}

func TestRememberRefusesWordsThePersonDidNotType(t *testing.T) {
	for _, quote := range []string{"never run cargo with more than 3 jobs", "the build is make all", "prove it end to end", "today", "the logs go to /tmp"} {
		tool, asked := rememberChain(t, turn.PersonAllowedOnce)
		result, err := remembered(t, tool, "cargo runs with 2 jobs", quote, "person")
		shelves := kept(t, tool)
		if err == nil || *asked != 0 || len(shelves.Global.Entries)+len(shelves.Project.Entries) != 0 {
			t.Errorf("quote %q: result %q, err %v, asked %d times, %d entries; want a refusal before asking and nothing kept", quote, result.Content, err, *asked, len(shelves.Global.Entries))
		}
	}
}

func TestRememberAsksAndKeepsWordsFromAnyMessageOfTheChain(t *testing.T) {
	tool, asked := rememberChain(t, turn.PersonAllowedOnce)
	result, err := remembered(t, tool, "cargo runs with at most 2 jobs", "never run cargo with more than 2 jobs", "person")
	shelves := kept(t, tool)
	if err != nil || *asked != 1 || len(shelves.Project.Entries) != 1 || !strings.Contains(result.Content, "[memory#m1]") || result.Command != "[memory#m1]" {
		t.Fatalf("result %+v, err %v, asked %d, project %d: want one ask and m1 kept in the project the answer chose", result, err, *asked, len(shelves.Project.Entries))
	}
	if entry := shelves.Project.Entries[0]; entry.Said != "never run cargo with more than 2 jobs" || entry.By != memory.ByLead {
		t.Errorf("kept %+v, want the quote as said and by lead", entry)
	}
	if _, err := remembered(t, tool, "replies stay short", "and keep replies short", "project"); err != nil {
		t.Fatalf("words typed in a later session, with other spacing, were refused: %v", err)
	}
	if shelves := kept(t, tool); len(shelves.Project.Entries) != 2 {
		t.Errorf("a second project answer went to %d project entries", len(shelves.Project.Entries))
	}
}

func TestRememberKeepsNothingOnANoOrWithNobodyToAsk(t *testing.T) {
	tool, _ := rememberChain(t, turn.PersonDenied)
	if result, err := remembered(t, tool, "cargo runs with 2 jobs", "never run cargo", "person"); err != nil || !strings.Contains(result.Content, "no") {
		t.Errorf("a no gave %q, %v", result.Content, err)
	}
	tool.Ask = nil
	if result, err := remembered(t, tool, "cargo runs with 2 jobs", "never run cargo", "person"); err != nil || !strings.Contains(result.Content, "nothing was kept") {
		t.Errorf("nobody to ask gave %q, %v", result.Content, err)
	}
	if shelves := kept(t, tool); len(shelves.Global.Entries)+len(shelves.Project.Entries) != 0 {
		t.Errorf("a no kept %d entries", len(shelves.Global.Entries))
	}
}

func TestRememberWritesWithoutAskingOnceAutoMemoryIsOn(t *testing.T) {
	tool, asked := rememberChain(t, turn.PersonDenied)
	tool.Auto = func() bool { return true }
	if _, err := remembered(t, tool, "cargo runs with 2 jobs", "never run cargo", "person"); err != nil || *asked != 0 {
		t.Fatalf("auto memory asked %d times, err %v", *asked, err)
	}
	if shelves := kept(t, tool); len(shelves.Global.Entries) != 1 {
		t.Errorf("auto memory kept %d entries", len(shelves.Global.Entries))
	}
}
