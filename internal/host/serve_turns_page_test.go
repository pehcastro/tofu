package host

import (
	"encoding/json"
	"testing"
	"time"

	"tofu/internal/session"
	"tofu/internal/turn"
)

func TestServeTurnsPageOneGenerationWithTypedPreviews(t *testing.T) {
	c, _, project := serving(t, nil, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	name, at := "page-test", time.Now().Add(-time.Hour)
	prompt := "<env>\nworking directory F:/x\n</env>" + turn.TheTaskFollows + "fix the parser\nand the lexer too"
	record := func(header session.Header, id, task, said string) {
		log, err := store.Open(header)
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct {
			kind session.EventKind
			body any
		}{
			{session.EventTurnStart, session.TurnStart{Task: task}},
			{session.EventMessage, session.MessageBody{Role: session.RoleUser, Content: task}},
			{session.EventMessage, session.MessageBody{Role: session.RoleAssistant, Content: said}},
			{session.EventTurnEnd, turn.Row{}},
		} {
			if _, err := log.Append(session.Event{Kind: step.kind, Turn: id, At: at}, step.body); err != nil {
				t.Fatal(err)
			}
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	older, newer := session.NewEventID(), session.NewEventID()
	record(session.Header{ID: older, Root: older, Name: &name, At: at, ForkedInto: newer}, "turn-old", "the older ask", "older reply")
	record(session.Header{ID: newer, Root: older, Name: &name, At: at.Add(time.Minute), CarriedFrom: &session.Carried{Session: older}}, "turn-new", prompt, "done here\nwith a long tail")

	read := func(id, method, params string, into any) {
		c.ask(id, method, params)
		line := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id)
		if line.Error != nil || json.Unmarshal(line.Result, into) != nil {
			t.Fatalf("%s %s answered %s and %+v", method, params, line.Result, line.Error)
		}
	}
	var page SessionTurns
	read("2", "session.turns", `{"session":"`+name+`"}`, &page)
	if page.Session != newer || page.Next != older || len(page.Turns) != 1 || page.Turns[0].Asked != "fix the parser" || page.Turns[0].Summary != "done here" || page.Turns[0].Prompt != "" {
		t.Errorf("session.turns by name answered %+v, want the newest generation %s alone, next %s, asked the typed first line and summary its first line", page, newer, older)
	}
	var earlier SessionTurns
	read("3", "session.turns", `{"session":"`+page.Next+`"}`, &earlier)
	if earlier.Session != older || earlier.Next != "" || len(earlier.Turns) != 1 || earlier.Turns[0].Turn != "turn-old" {
		t.Errorf("session.turns on the next page answered %+v, want only turn-old and no next", earlier)
	}
	var whole TurnDigest
	read("4", "session.turn", `{"session":"`+newer+`","turn":"turn-new"}`, &whole)
	if whole.Prompt != prompt || whole.Summary != "done here\nwith a long tail" || whole.Asked != "fix the parser\nand the lexer too" {
		t.Errorf("session.turn answered %+v, want the whole prompt, the whole summary and the whole typed text", whole)
	}
}
