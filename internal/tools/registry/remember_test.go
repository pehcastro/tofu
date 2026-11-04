package registry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/registry"
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

type chain struct {
	tool    registry.Remember
	picking context.Context
	offered []registry.MemoryOffer
}

func rememberChain(t *testing.T, answer entries.Scope) *chain {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	store := session.NewStore(t.TempDir())
	if err := store.Write(session.Header{ID: olderSession, Root: olderSession, At: time.Now()}, []session.Event{
		said(t, "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2201", "task", "<env>\ntoday\n</env>"+loop.TheTaskFollows+"never run cargo with more than 2 jobs, ok?"+loop.NotesAfterTheTask+"[code_rules, from the rule e2e_first]\nprove it end to end"),
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
	c := &chain{tool: registry.Remember{Store: store, Session: newerSession, Project: t.TempDir(), Inbox: loop.NewInbox(), Auto: func() bool { return false }}}
	c.picking = registry.WithMemoryPick(context.Background(), func(_ context.Context, offer registry.MemoryOffer) (entries.Scope, error) {
		c.offered = append(c.offered, offer)
		return answer, nil
	})
	return c
}

func (c *chain) remembered(t *testing.T, statement, quote, kind string) (contract.Result, error) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"statement": statement, "said": quote, "kind": kind})
	if err != nil {
		t.Fatal(err)
	}
	return c.tool.Run(c.picking, args)
}

func (c *chain) kept(t *testing.T) entries.Memory {
	t.Helper()
	shelves, err := entries.Open(c.tool.Project)
	if err != nil {
		t.Fatal(err)
	}
	return shelves
}

func TestRememberRefusesWordsThePersonDidNotType(t *testing.T) {
	for _, quote := range []string{"never run cargo with more than 3 jobs", "the build is make all", "prove it end to end", "today"} {
		c := rememberChain(t, entries.Project)
		result, err := c.remembered(t, "cargo runs with 2 jobs", quote, "person")
		shelves := c.kept(t)
		if err == nil || len(c.offered) != 0 || len(shelves.Global.Entries)+len(shelves.Project.Entries) != 0 {
			t.Errorf("quote %q: result %q, err %v, asked %d times, %d entries; want a refusal before asking and nothing kept", quote, result.Content, err, len(c.offered), len(shelves.Global.Entries))
		}
	}
}

func TestRememberTakesFragmentsOfOneMessageInTheirOrder(t *testing.T) {
	for quote, takes := range map[string]bool{
		"ok. ... the logs go to /tmp":                 true,
		"also remember…/tmp":                          true,
		"...keep replies short":                       true,
		"never run cargo ... 2 jobs, ok?":             true,
		"the logs go to /tmp ... ok. also":            false,
		"keep replies ... the logs go to /tmp":        false,
		"remember ... remember":                       false,
		"...":                                         false,
		"never run cargo ... the logs go to /tmp":     false,
		"ok. also remember that ... the logs go to /": true,
	} {
		_, err := rememberChain(t, entries.Project).remembered(t, "logs go to /tmp", quote, "project")
		if takes != (err == nil) {
			t.Errorf("quote %q: err %v, want taken %v", quote, err, takes)
		}
	}
}

func TestRememberAsksAndKeepsWordsFromAnyMessageOfTheChain(t *testing.T) {
	c := rememberChain(t, entries.Project)
	result, err := c.remembered(t, "cargo runs with at most 2 jobs", "never run cargo with more than 2 jobs", "person")
	shelves := c.kept(t)
	if err != nil || len(c.offered) != 1 || len(shelves.Project.Entries) != 1 || !strings.Contains(result.Content, "[memory#m1]") || result.Command != "[memory#m1]" {
		t.Fatalf("result %+v, err %v, asked %d, project %d: want one ask and m1 kept in the project the answer chose", result, err, len(c.offered), len(shelves.Project.Entries))
	}
	if entry := shelves.Project.Entries[0]; entry.Said != "never run cargo with more than 2 jobs" || entry.By != entries.ByLead {
		t.Errorf("kept %+v, want the quote as said and by lead", entry)
	}
	if _, err := c.remembered(t, "replies stay short", "and keep replies short", "project"); err != nil {
		t.Fatalf("words typed in a later session, with other spacing, were refused: %v", err)
	}
	if shelves := c.kept(t); len(shelves.Project.Entries) != 2 {
		t.Errorf("a second project answer went to %d project entries", len(shelves.Project.Entries))
	}
}

func TestRememberKeepsTheScopeThePersonPickedOnTheCard(t *testing.T) {
	c := rememberChain(t, entries.UserLocal)
	if _, err := c.remembered(t, "cargo runs with at most 2 jobs", "never run cargo with more than 2 jobs", "person"); err != nil {
		t.Fatal(err)
	}
	if shelves := c.kept(t); len(shelves.UserLocal.Entries) != 1 || len(shelves.Global.Entries) != 0 {
		t.Errorf("the person picked user-local and it went to %d user-local, %d user-global", len(shelves.UserLocal.Entries), len(shelves.Global.Entries))
	}
	if offer := c.offered[0]; offer.Scope != entries.Global || len(offer.Scopes) != 3 {
		t.Errorf("with no Jev the card offered %+v, want the person kind's user-global preselected and project local left out", offer)
	}
}

func TestRememberRefusesAProjectLocalPickJevCouldNotClear(t *testing.T) {
	c := rememberChain(t, entries.ProjectLocal)
	result, err := c.remembered(t, "cargo runs with at most 2 jobs", "never run cargo with more than 2 jobs", "person")
	if shelves := c.kept(t); err != nil || !strings.Contains(result.Content, "refused project local") || len(shelves.ProjectLocal.Entries) != 0 {
		t.Errorf("a project-local pick with no Jev answer gave %q, %v, and kept %d project-local entries", result.Content, err, len(shelves.ProjectLocal.Entries))
	}
}

func TestRememberKeepsNothingOnANoOrWithNobodyToAsk(t *testing.T) {
	c := rememberChain(t, "")
	if result, err := c.remembered(t, "cargo runs with 2 jobs", "never run cargo", "person"); err != nil || !strings.Contains(result.Content, "no") {
		t.Errorf("a no gave %q, %v", result.Content, err)
	}
	c.picking = context.Background()
	if result, err := c.remembered(t, "cargo runs with 2 jobs", "never run cargo", "person"); err != nil || !strings.Contains(result.Content, "nothing was kept") {
		t.Errorf("nobody to ask gave %q, %v", result.Content, err)
	}
	if shelves := c.kept(t); len(shelves.Global.Entries)+len(shelves.Project.Entries) != 0 {
		t.Errorf("a no kept %d entries", len(shelves.Global.Entries))
	}
}

func TestRememberWritesWithoutAskingOnceAutoMemoryIsOn(t *testing.T) {
	c := rememberChain(t, "")
	c.tool.Auto = func() bool { return true }
	var told []registry.MemoryKept
	c.picking = registry.WithMemoryKept(c.picking, func(kept registry.MemoryKept) { told = append(told, kept) })
	if _, err := c.remembered(t, "cargo runs with 2 jobs", "never run cargo", "person"); err != nil || len(c.offered) != 0 {
		t.Fatalf("auto memory asked %d times, err %v", len(c.offered), err)
	}
	if shelves := c.kept(t); len(shelves.Global.Entries) != 1 {
		t.Errorf("auto memory kept %d entries", len(shelves.Global.Entries))
	}
	if len(told) != 1 || told[0] != (registry.MemoryKept{Statement: "cargo runs with 2 jobs", Offered: entries.Global, Kept: entries.Global}) {
		t.Errorf("auto memory told the host %+v, want one keep of user-global offered and kept", told)
	}
}
