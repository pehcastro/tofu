package boardy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAcceptRunsTheCreateGate(t *testing.T) {
	m := managedBoard(t, "DEMO")
	if _, err := m.Create("DEMO", Ticket{Front: Front{Title: "asked bare"}, Problem: "p", Scope: "s", Acceptance: []string{"- a line"}}, agent); err == nil {
		t.Fatal("an agent's request with no sections reached triage")
	}
	legacy := Request{ID: "T7", By: *m.Store.authorOf(agent), Ticket: Ticket{Front: Front{Title: "asked before the sections"}, Problem: "p", Scope: "s", Acceptance: []string{"- a line"}}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(m.Store.BoardDir("DEMO"), TriageDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Store.requestPath("DEMO", "T7"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = m.Accept("DEMO", "T7", manager)
	refused(t, err, "Driven path")
	if _, statErr := os.Stat(m.Store.requestPath("DEMO", "T7")); statErr != nil {
		t.Fatalf("a refused accept removed the request: %v", statErr)
	}
	if _, err := m.Create("DEMO", ticketWith("asked whole"), agent); err != nil {
		t.Fatal(err)
	}
	requests, err := m.Store.Requests("DEMO")
	if err != nil || len(requests) != 2 {
		t.Fatalf("triage holds %d requests, %v, want 2", len(requests), err)
	}
	accepted, err := m.Accept("DEMO", requests[0].ID, manager)
	if err != nil || accepted.Driven != "d" || accepted.Feasibility != "f" {
		t.Fatalf("a request carrying its sections accepted as %+v, %v", accepted, err)
	}
	if _, err := m.RedraftRequest("DEMO", "T7", "before", "b", agent); err == nil {
		t.Fatal("an agent filled a request")
	}
	if _, err := m.RedraftRequest("DEMO", "T9", "before", "b", manager); err == nil {
		t.Fatal("a request that does not exist was filled")
	}
	for field, body := range map[string]string{"before": "b", "driven": "d", "still_works": "w", "error_case": "e", "feasibility": "f"} {
		if _, err := m.RedraftRequest("DEMO", "T7", field, body, manager); err != nil {
			t.Fatal(err)
		}
	}
	filled, err := m.Store.readRequest("DEMO", "T7")
	if err != nil || filled.By.ID != legacy.By.ID || filled.Ticket.Title != legacy.Ticket.Title || filled.Ticket.StillWorks != "w" {
		t.Fatalf("the filled request reads back as %+v, %v", filled, err)
	}
	if made, err := m.Accept("DEMO", "T7", manager); err != nil || made.ErrorCase != "e" {
		t.Fatalf("the filled request accepted as %+v, %v", made, err)
	}
}

func TestATicketInTriageStatusRunsTheCreateGate(t *testing.T) {
	m := managedBoard(t, "DEMO")
	bare := func(title string) Ticket {
		made, err := m.Store.Create("DEMO", Ticket{Front: Front{Title: title, Status: Triage}, Problem: "p", Scope: "s", Acceptance: []string{"- a line"}})
		if err != nil {
			t.Fatal(err)
		}
		return made
	}
	asked := bare("triaged before the sections")
	_, err := m.Accept("DEMO", asked.ID, manager)
	refused(t, err, "Empty or error case")
	_, err = m.Move(asked.ID, Backlog, "", manager)
	refused(t, err, "Before")
	if dropped, err := m.Move(bare("dropped while bare").ID, Dropped, "a duplicate", manager); err != nil || dropped.Status != Dropped {
		t.Fatalf("a bare triaged ticket could not be dropped: %v", err)
	}
	whole := ticketWith("triaged whole")
	whole.Status = Triage
	made, err := m.Store.Create("DEMO", whole)
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(made.ID, Backlog, "", manager); err != nil || moved.Status != Backlog {
		t.Fatalf("a triaged ticket carrying its sections moved as %v, %v", moved.Status, err)
	}
}
