package boardy

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	person  = "luiz"
	manager = "s-manager"
	agent   = "s-agent"
	other   = "s-other"
)

func managedBoard(t *testing.T, keys ...string) Managed {
	t.Helper()
	m := Managed{Local: Local{Store: Store{Dir: t.TempDir()}}, Person: person}
	for _, key := range keys {
		if _, err := m.Store.Init(key, key); err != nil {
			t.Fatal(err)
		}
		if _, err := m.AddManager(key, manager, person); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func ticketWith(title string, owns ...string) Ticket {
	return Ticket{Front: Front{Title: title, Owns: owns}, Problem: "p", Scope: "s", Before: "b", Driven: "d", StillWorks: "w", ErrorCase: "e", Feasibility: "f", Acceptance: []string{"- a line"}}
}

func proved(t *testing.T, m Managed, id string) {
	t.Helper()
	if _, err := m.Evidence(id, "ran it and read what it printed", manager, 0); err != nil {
		t.Fatal(err)
	}
}

func boardRule(t *testing.T, m Managed, name, body string) {
	t.Helper()
	dir := filepath.Join(m.Store.BoardDir("DEMO"), RulesDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func refused(t *testing.T, err error, says string) {
	t.Helper()
	var refusal RefusedError
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), says) {
		t.Fatalf("want a refusal saying %q, got %v", says, err)
	}
}

func assigned(t *testing.T, m Managed, title, to string, status Status) Ticket {
	t.Helper()
	created, err := m.Create("DEMO", ticketWith(title), manager)
	if err != nil {
		t.Fatal(err)
	}
	created, err = m.Store.Edit(created.ID, func(ticket *Ticket) error {
		ticket.Assignee, ticket.Status = to, status
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestTransitionAgentConditions(t *testing.T) {
	m := managedBoard(t, "DEMO")
	mine := assigned(t, m, "mine", agent, Doing)
	theirs := assigned(t, m, "theirs", other, Doing)

	_, err := m.Move(theirs.ID, Review, "", agent)
	refused(t, err, other)
	_, err = m.Move(mine.ID, Done, "", agent)
	refused(t, err, "done")
	_, err = m.Move(mine.ID, Review, "", agent)
	refused(t, err, "Log")

	if _, err := m.Comment(mine.ID, "go test passed", agent); err != nil {
		t.Fatal(err)
	}
	moved, err := m.Move(mine.ID, Review, "", agent)
	if err != nil || moved.Status != Review {
		t.Fatalf("an agent's own ticket with evidence moves to review: %v %v", moved.Status, err)
	}
}

func TestTransitionTriage(t *testing.T) {
	m := managedBoard(t, "DEMO")
	requested, err := m.Create("DEMO", ticketWith("asked for"), agent)
	if err != nil || requested.ID != "" || requested.Status != Triage {
		t.Fatalf("an agent's new is a triage request with no number, got %q %q %v", requested.ID, requested.Status, err)
	}
	requests, err := m.Store.Requests("DEMO")
	if err != nil || len(requests) != 1 {
		t.Fatalf("one request, got %v %v", requests, err)
	}
	_, err = m.Accept("DEMO", requests[0].ID, agent)
	refused(t, err, "manager")
	accepted, err := m.Accept("DEMO", requests[0].ID, manager)
	if err != nil || accepted.ID != "DEMO-1" {
		t.Fatalf("the manager's accept numbers it DEMO-1, got %q %v", accepted.ID, err)
	}
	if _, err := m.Accept("DEMO", requests[0].ID, manager); err == nil {
		t.Fatal("accepting the same request twice passed")
	}
}

func TestTransitionWidenAndReferences(t *testing.T) {
	m := managedBoard(t, "DEMO", "OPS")
	doing := ticketWith("doing", "internal/rule/**")
	doing.Status = Doing
	live, err := m.Create("OPS", doing, manager)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := m.Create("OPS", ticketWith("closed", "cmd/boji/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, closed.ID)
	if _, err := m.Move(closed.ID, Done, "", manager); err != nil {
		t.Fatal(err)
	}
	ours := ticketWith("mine", "docs/**")
	ours.Status = Doing
	mine, err := m.Create("DEMO", ours, manager)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Widen(mine.ID, []string{"internal/rule/load.go"}, manager)
	refused(t, err, live.ID)
	if _, err := m.Widen(mine.ID, []string{"cmd/boji/main.go"}, manager); err != nil {
		t.Fatalf("a done ticket's owns do not block a widen: %v", err)
	}
	_, err = m.Widen(mine.ID, []string{"library/**"}, agent)
	refused(t, err, "owns")

	dangling := ticketWith("dangling")
	dangling.Depends = []string{"DEMO-99"}
	_, err = m.Create("DEMO", dangling, manager)
	refused(t, err, "DEMO-99")
}

func TestTransitionRules(t *testing.T) {
	m := managedBoard(t, "DEMO")
	boardRule(t, m, "person_closes@1.yaml", "id: person_closes\ndomain: general\nkind: structural\nconcern: process_discipline\nchecker: person_approves\nmode: enforced\non: close\nto: done\n")
	ticket := assigned(t, m, "close me", agent, Review)
	proved(t, m, ticket.ID)
	_, err := m.Move(ticket.ID, Done, "", manager)
	var waiting WaitingError
	if !errors.As(err, &waiting) {
		t.Fatalf("a manager's close waits for the person, got %v", err)
	}
	if still, _ := m.Store.Get(ticket.ID); still.Status != Review {
		t.Fatalf("a waiting close moved the ticket to %s", still.Status)
	}
	if closed, err := m.Move(ticket.ID, Done, "", person); err != nil || closed.Status != Done {
		t.Fatalf("the person's close passes: %v", err)
	}

	boardRule(t, m, "test_line@1.yaml", "id: test_line\ndomain: general\nkind: structural\nconcern: process_discipline\nchecker: command\ncommand: go nosuchverb {ticket}\nmode: enforced\ntext: a ticket names its test before review\non: move\nto: review\n")
	next := assigned(t, m, "no test", agent, Doing)
	if _, err := m.Comment(next.ID, "evidence", agent); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(next.ID, Review, "", agent)
	refused(t, err, "a ticket names its test before review")

	boardRule(t, m, "test_line@1.yaml", "id: test_line\ndomain: general\nkind: structural\nconcern: process_discipline\nchecker: command\ncommand: go nosuchverb {ticket}\nmode: shadow\ntext: a ticket names its test before review\non: move\nto: review\n")
	if _, err := m.Move(next.ID, Review, "", agent); err != nil {
		t.Fatalf("a shadow rule does not block: %v", err)
	}

	boardRule(t, m, "bad@1.yaml", "id: bad\ndomain: general\nkind: structural\nconcern: process_discipline\nchecker: person_approves\non: move\nto: nowhere\n")
	if _, err := m.Move(next.ID, Doing, "", manager); err == nil || !strings.Contains(err.Error(), "bad@1.yaml") {
		t.Fatalf("a rule naming no status is refused naming its file, got %v", err)
	}
}

func TestTransitionHand(t *testing.T) {
	m := managedBoard(t, "DEMO", "OPS")
	ticket := assigned(t, m, "hand me", agent, Doing)
	if _, err := m.Comment(ticket.ID, "first finding", manager); err != nil {
		t.Fatal(err)
	}
	handed, err := m.Hand(ticket.ID, "OPS", m.Store, manager)
	if err != nil || handed.ID != "OPS-1" || !strings.Contains(handed.Log, "first finding") {
		t.Fatalf("OPS-1 carries the Log, got %q %q %v", handed.ID, handed.Log, err)
	}
	old, _ := m.Store.Get(ticket.ID)
	if old.Status != Dropped || !strings.Contains(old.Reason, "OPS-1") {
		t.Fatalf("the old id points at OPS-1, got %s %q", old.Status, old.Reason)
	}
	if _, err := m.Hand(ticket.ID, "OPS", m.Store, manager); err == nil {
		t.Fatal("handing a handed ticket again passed")
	}
}

func TestHookFollowUpRunsTheCreateGate(t *testing.T) {
	m := managedBoard(t, "DEMO")
	boardRule(t, m, "follow.yaml", "id: follow\nwhen: after\non: ticket.create\ndo: create follow up {ticket}\n")
	boardRule(t, m, "logged.yaml", "id: logged\nwhen: after\non: ticket.create\ndo: log made\n")
	sizes := m.Store.EventSizes()
	made, err := m.Create("DEMO", ticketWith("leads to a follow up"), manager)
	if err != nil {
		t.Fatal(err)
	}
	if said := m.Store.NudgesSince(sizes); len(said) != 1 || !strings.Contains(said[0], "follow") || !strings.Contains(said[0], "Driven path") {
		t.Fatalf("the create that triggered a refused follow up said %q, want one line naming the refusal", said)
	}
	if tickets, _ := m.Store.Tickets("DEMO"); len(tickets) != 1 || tickets[0].ID != made.ID {
		t.Fatalf("the board holds %d tickets after a follow up with no sections, want only %s", len(tickets), made.ID)
	}
	events, err := m.Store.ticketEvents(made.ID)
	if err != nil || !slices.ContainsFunc(events, func(e Event) bool {
		return e.Kind == RuleFired && strings.Contains(e.Text, "did not run") && strings.Contains(e.Text, "Driven path")
	}) {
		t.Fatalf("no event says why the follow up was not made: %+v, %v", events, err)
	}
}

func TestSetRedraftsASection(t *testing.T) {
	m := managedBoard(t, "DEMO")
	made, err := m.Create("DEMO", ticketWith("redrafted"), manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Redraft(made.ID, "still_works", "list --json\nthe sort key", agent); err == nil {
		t.Fatal("an agent redrafted a section")
	}
	if _, err := m.Redraft(made.ID, "before", "  ", manager); err == nil {
		t.Fatal("a section was set to nothing")
	}
	if _, err := m.Redraft(made.ID, "nowhere", "x", manager); err == nil {
		t.Fatal("a section that does not exist was set")
	}
	redrafted, err := m.Redraft(made.ID, "still_works", "list --json\nthe sort key", manager)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := m.Store.Get(made.ID)
	if err != nil || stored.StillWorks != "list --json\nthe sort key" || stored.Before != "b" || !strings.Contains(redrafted.Log, "set Still works") {
		t.Fatalf("the redraft read back as %q %q, log %q, %v", stored.StillWorks, stored.Before, redrafted.Log, err)
	}
}

func TestUnboundClassifierSaysHowToBindIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m := managedBoard(t, "DEMO")
	unjudged := ticketWith("no feasibility")
	unjudged.Feasibility = ""
	const bind = "tofu models bind classifier"
	if _, err := m.Create("DEMO", unjudged, manager); err == nil || !strings.Contains(err.Error(), bind) {
		t.Fatalf("a lead's create held for an unbound classifier: %v, want it to name %s", err, bind)
	}
	sizes := m.Store.EventSizes()
	if _, err := m.Create("DEMO", unjudged, person); err != nil {
		t.Fatal(err)
	}
	said := m.Store.NudgesSince(sizes)
	if len(said) != 1 || !strings.Contains(said[0], bind) || !strings.Contains(said[0], feasibilityRef) {
		t.Fatalf("the person's create with no classifier said %q, want one line naming %s", said, bind)
	}
	made, err := m.Create("DEMO", ticketWith("closed by the person"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, made.ID)
	sizes = m.Store.EventSizes()
	if _, err := m.Move(made.ID, Done, "driven and passing", person); err != nil {
		t.Fatal(err)
	}
	said = m.Store.NudgesSince(sizes)
	if len(said) != 1 || !strings.Contains(said[0], bind) || !strings.Contains(said[0], doneReasonRef) {
		t.Fatalf("the person's done with no classifier said %q, want one line naming %s", said, bind)
	}
	if thread, err := m.Store.Thread(made.ID); err != nil || len(thread) == 0 {
		t.Fatalf("the ticket's thread after the notes reads %d events, %v", len(thread), err)
	}
}

func TestCreateNeedsTheSectionsAPersonDrives(t *testing.T) {
	m := managedBoard(t, "DEMO")
	sections := []string{"Before", "Driven path", "Still works", "Empty or error case"}
	bare := Ticket{Front: Front{Title: "bare"}, Problem: "p", Scope: "s", Acceptance: []string{"- a line"}}
	for _, actor := range []string{manager, agent} {
		_, err := m.Create("DEMO", bare, actor)
		for _, section := range sections {
			refused(t, err, section)
		}
	}
	blank := ticketWith("blank error case")
	blank.ErrorCase = " \n "
	_, err := m.Create("DEMO", blank, manager)
	refused(t, err, "Empty or error case")
	for _, present := range sections[:3] {
		if strings.Contains(err.Error(), present) {
			t.Errorf("a ticket carrying %s was refused naming it: %v", present, err)
		}
	}
	unjudged := ticketWith("no feasibility")
	unjudged.Feasibility = ""
	var waiting WaitingError
	if _, err := m.Create("DEMO", unjudged, manager); !errors.As(err, &waiting) || !strings.Contains(err.Error(), "ticket_feasibility@1") {
		t.Fatalf("a lead's ticket Jev could not judge for feasibility: %v, want it held for the person", err)
	}
	if _, err := m.Create("DEMO", unjudged, person); err != nil {
		t.Fatalf("the person's own ticket Jev could not judge: %v, want it made", err)
	}
	made, err := m.Create("DEMO", ticketWith("full"), manager)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := m.Store.Get(made.ID)
	if err != nil || stored.Before != "b" || stored.Driven != "d" || stored.StillWorks != "w" || stored.ErrorCase != "e" || stored.Feasibility != "f" {
		t.Fatalf("the sections read back as %+v, %v", stored, err)
	}
}
