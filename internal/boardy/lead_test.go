package boardy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sessionBoard(t *testing.T, managers ...string) (Managed, Ticket) {
	t.Helper()
	m := Managed{Local: Local{Store: Store{Dir: t.TempDir()}}}
	board, err := m.Store.Init("DEMO", "demo")
	if err != nil {
		t.Fatal(err)
	}
	board.Managers = managers
	if err := writeAtomic(board.Dir+"/"+BoardFileName, board.toml()); err != nil {
		t.Fatal(err)
	}
	created, err := m.Local.Create("DEMO", ticketWith("the work", "src/**"), "test")
	if err != nil {
		t.Fatal(err)
	}
	return m, created
}

func TestALeadSessionActsForThePersonAndItsSubAgentDoesNot(t *testing.T) {
	const lead = "88fbfa43-1d2e-4c3b-9a0f-5e6d7c8b9a01"
	sub := lead + "/DEMO-1"

	m, ticket := sessionBoard(t)
	if _, err := m.Assign(ticket.ID, sub, sub); err == nil {
		t.Error("a sub-agent assigned a ticket on a board with no managers")
	}
	if _, err := m.Assign(ticket.ID, sub, lead); err != nil {
		t.Fatalf("the lead on a board with no managers: %v", err)
	}
	if _, err := m.Move(ticket.ID, Doing, "", lead); err != nil {
		t.Fatalf("the lead moving to doing: %v", err)
	}
	if _, err := m.Move(ticket.ID, Done, "", sub); err == nil {
		t.Error("a sub-agent moved its own ticket to done")
	}
	if event := m.boardEvent("move", ticket, Review, lead); !event.ByPerson {
		t.Error("a lead's move is not by the person")
	}
	if event := m.boardEvent("move", ticket, Review, sub); event.ByPerson {
		t.Error("a sub-agent's move is by the person")
	}

	listed, listedTicket := sessionBoard(t, string(RolePerson))
	if _, err := listed.Assign(listedTicket.ID, sub, lead); err != nil {
		t.Errorf("the lead on a board that lists the person: %v", err)
	}

	other, otherTicket := sessionBoard(t, "a-different-session")
	_, err := other.Assign(otherTicket.ID, sub, lead)
	refused(t, err, "only a manager may assign")

	named := managedBoard(t, "DEMO")
	created, err := named.Create("DEMO", ticketWith("theirs"), manager)
	if err != nil {
		t.Fatal(err)
	}
	_, err = named.Assign(created.ID, agent, agent)
	refused(t, err, "only a manager may assign")
}

func TestLintRefusesTwoWildcardGlobsInOneFolder(t *testing.T) {
	m, first := sessionBoard(t)
	if _, err := m.Store.Edit(first.ID, func(ticket *Ticket) error { ticket.Owns = []string{"x/*.go"}; return nil }); err != nil {
		t.Fatal(err)
	}
	second, err := m.Local.Create("DEMO", ticketWith("the tests", "x/*_test.go"), "test")
	if err != nil {
		t.Fatal(err)
	}
	finished, err := m.Local.Create("DEMO", Ticket{Front: Front{Title: "finished", Status: Done, Owns: []string{"x/a.go"}}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	boards, _ := m.Store.Boards()
	var overlaps []string
	for _, finding := range m.Store.Lint(boards) {
		if strings.Contains(finding.What, "overlap") {
			overlaps = append(overlaps, finding.What)
		}
	}
	if len(overlaps) != 1 || !strings.Contains(overlaps[0], second.ID) || strings.Contains(overlaps[0], finished.ID) {
		t.Errorf("lint found %q, want one overlap between %s and %s", overlaps, first.ID, second.ID)
	}
}

func TestAPlanDayReadsBackAsTheDayWritten(t *testing.T) {
	m, _ := sessionBoard(t)
	store := m.Store
	if err := writeAtomic(store.BoardDir("DEMO")+"/milestones/M1.md", []byte("---\nid: M1\ntitle: Launch\ndue: 2026-11-08\n---\n")); err != nil {
		t.Fatal(err)
	}
	milestones, err := store.Milestones("DEMO")
	if err != nil || len(milestones) != 1 {
		t.Fatalf("read %d milestones: %v", len(milestones), err)
	}
	raw, _ := json.Marshal(milestones[0])
	if !strings.Contains(string(raw), `"due":"2026-11-08"`) {
		t.Errorf("a due of 2026-11-08 reads back as %s", raw)
	}
	if err := writeAtomic(store.BoardDir("DEMO")+"/milestones/M2.md", []byte("---\nid: M2\ntitle: Later\ndue: 2026-13-40\n---\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Milestones("DEMO"); err == nil {
		t.Error("a due of 2026-13-40 parsed without complaint")
	}
	if err := store.SaveSprint("DEMO", Sprint{ID: "S1", Title: "First", State: Planned}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSprint("DEMO", "S1", Active); err != nil {
		t.Fatal(err)
	}
	sprints, _ := store.Sprints("DEMO")
	if raw, _ := json.Marshal(sprints); !strings.Contains(string(raw), `"start":"`+time.Now().Format(time.DateOnly)+`"`) {
		t.Errorf("a sprint started today reads %s, want today in this zone", raw)
	}
}
