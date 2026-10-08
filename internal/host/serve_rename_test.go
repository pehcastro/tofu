package host

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/session"
)

func TestSessionRenameNamesTheWholeFamilyAndLeavesTheOpenSessionWhereItWas(t *testing.T) {
	c, _, project := serving(t, nil, ServeConfig{})
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	first := session.Header{ID: session.NewEventID(), At: time.Now().Add(-time.Hour)}
	recordSession(t, store, first)
	second := session.Header{ID: session.NewEventID(), At: time.Now(), Root: first.ID, ForkKind: "continuation", CarriedFrom: &session.Carried{Session: first.ID}}
	recordSession(t, store, second)
	log, err := store.Open(session.Header{ID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(log.Edit(func(header *session.Header) { header.ForkedInto = second.ID }), log.Close()); err != nil {
		t.Fatal(err)
	}

	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)

	c.ask("3", "session.rename", `{"session":"`+second.ID+`","name":"The Gate Work"}`)
	var updated SessionUpdated
	c.until(func(line wireLine) bool {
		return line.Method == "session.updated" && json.Unmarshal(line.Params, &updated) == nil && updated.Session == second.ID
	}, "session.updated for the renamed session")
	if updated.Name != "the-gate-work" || !strings.HasPrefix(updated.Handle, "the-gate-work") || updated.Generation != 2 {
		t.Errorf("session.updated after the rename = %+v, want the new name, its handle and generation 2", updated)
	}
	var ack Ack
	c.answer("3", &ack)
	for _, id := range []string{first.ID, second.ID} {
		if header, err := store.Header(id); err != nil || header.Named() != "the-gate-work" {
			t.Errorf("generation %s is called %q after the rename, %v", id, header.Named(), err)
		}
	}

	c.ask("4", "cron.command", `{"line":"/loop 10m check the build"}`)
	var cron CronUpdated
	c.until(func(line wireLine) bool {
		return line.Method == "cron.updated" && json.Unmarshal(line.Params, &cron) == nil
	}, "cron.updated")
	if cron.Session != opened.Session {
		t.Errorf("after renaming another session, events name %q rather than the open %q", cron.Session, opened.Session)
	}

	for id, params := range map[string]string{
		"5": `{"session":"` + first.ID + `","name":"!!!"}`,
		"6": `{"session":"turn-nowhere","name":"gate"}`,
		"7": `{"session":"` + first.ID + `","name":"gate","extra":1}`,
	} {
		c.ask(id, "session.rename", params)
		if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id); refused.Error == nil {
			t.Errorf("session.rename %s was accepted", params)
		}
	}
	if header, _ := store.Header(first.ID); header.Named() != "the-gate-work" {
		t.Errorf("a refused rename still renamed the family to %q", header.Named())
	}
	if !slices.ContainsFunc(requests(), func(one method) bool { return one.name == "session.rename" }) {
		t.Error("tofu serve --schema does not describe session.rename")
	}
}
