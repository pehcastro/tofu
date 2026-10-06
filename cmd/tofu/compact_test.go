package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	sessionstore "tofu/internal/session"
)

func TestCompactShrinksTheCarriedHistoryAndARestartCarriesItShrunk(t *testing.T) {
	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	log, err := store.Open(sessionstore.Header{ID: sessionstore.NewEventID()})
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("a numbered line of a large file\n", 400)
	for _, body := range []sessionstore.MessageBody{
		{Role: sessionstore.RoleUser, Content: "read a and b"},
		{Role: sessionstore.RoleAssistant, Content: "reading a", ToolCalls: []sessionstore.MessageToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}},
		{Role: sessionstore.RoleTool, ToolCallID: "a", Content: big},
		{Role: sessionstore.RoleAssistant, Content: "reading b", ToolCalls: []sessionstore.MessageToolCall{{ID: "b", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)}}},
		{Role: sessionstore.RoleTool, ToolCallID: "b", Content: big},
		{Role: sessionstore.RoleAssistant, Content: "both are numbered lines"},
	} {
		if _, err := log.Append(sessionstore.Event{Kind: sessionstore.EventMessage}, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHead(log.ID()); err != nil {
		t.Fatal(err)
	}
	resumed := continueCarry(store)
	live := newAppSession(t.TempDir(), nil, nil, time.Now, resumed)
	t.Cleanup(live.Close)

	said := live.compact()
	if !strings.Contains(said, "2 old tool result(s)") || live.ID() == log.ID() {
		t.Fatalf("/compact said %q and left the session at %s, want two results shrunk into a new session", said, live.ID())
	}
	restarted := continueCarry(store)
	if restarted.Session != live.ID() || len(restarted.messages) != len(resumed.messages) {
		t.Fatalf("a restart carries %s with %d messages, want %s with %d", restarted.Session, len(restarted.messages), live.ID(), len(resumed.messages))
	}
	for _, at := range []int{2, 4} {
		if content := restarted.messages[at].Content; content == big || !strings.Contains(content, "artifact ") {
			t.Errorf("after a restart message %d is %.80q, want the handle /compact left", at, content)
		}
	}
	if again := live.compact(); !strings.Contains(again, "nothing") {
		t.Errorf("a second /compact said %q, want that nothing was left to shrink", again)
	}
	if none := newAppSession(t.TempDir(), nil, nil, time.Now, sessionResume{}).compact(); none != compactNothingCarried {
		t.Errorf("/compact with nothing carried said %q", none)
	}
}
