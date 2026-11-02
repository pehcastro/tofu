package boardy

import (
	"testing"
)

func movedTo(t *testing.T, m Managed, id string, to Status) {
	t.Helper()
	ticket, err := m.Store.Edit(id, func(ticket *Ticket) error { ticket.Status = to; return nil })
	if err == nil {
		err = m.Store.Record(ticket.Board(), Event{Ticket: id, Kind: Moved, Actor: person, To: to})
	}
	if err != nil {
		t.Fatal(err)
	}
}

func standsAt(t *testing.T, m Managed, id string, want RequestState) RequestStanding {
	t.Helper()
	got, err := m.Store.RequestStanding("DEMO", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != want {
		t.Fatalf("request %s is %s, want %s: %s", id, got.State, want, got.Why)
	}
	return got
}

func TestRequestIsDoneOnlyWhenEveryTicketIsDoneAndItsReviewPasses(t *testing.T) {
	m := managedBoard(t, "DEMO", "OTHER")
	var cut []Ticket
	for _, title := range []string{"a", "b", "c"} {
		ticket, err := m.Store.Create("DEMO", ticketWith(title))
		if err != nil {
			t.Fatal(err)
		}
		cut = append(cut, ticket)
	}
	elsewhere, err := m.Store.Create("OTHER", ticketWith("x"))
	if err != nil {
		t.Fatal(err)
	}
	words := "---\nthe picker lists the themes and deletes one.\n  \"quoted\": twice  spaced\n"
	if _, err := m.NewRequest("DEMO", " \n ", []string{cut[0].ID}, person); err == nil {
		t.Fatal("a request with no words was made")
	}
	for _, wrong := range []string{"DEMO-99", elsewhere.ID, "nonsense"} {
		if _, err := m.NewRequest("DEMO", words, []string{wrong}, person); err == nil {
			t.Fatalf("a request holding %s was made", wrong)
		}
	}
	bare, err := m.NewRequest("DEMO", words, nil, person)
	if err != nil {
		t.Fatal(err)
	}
	standsAt(t, m, bare.Request.ID, RequestOpen)
	made, err := m.NewRequest("DEMO", words, []string{cut[0].ID, cut[1].ID, cut[0].ID}, person)
	if err != nil {
		t.Fatal(err)
	}
	id := made.Request.ID
	if id == bare.Request.ID {
		t.Fatalf("two requests share the id %s", id)
	}
	got := standsAt(t, m, id, RequestOpen)
	if got.Request.Words != words {
		t.Fatalf("the words came back as %q, not %q", got.Request.Words, words)
	}
	if len(got.Tickets) != 2 || got.Tickets[0].ID != cut[0].ID || got.Tickets[1].ID != cut[1].ID {
		t.Fatalf("the request holds %v, want %s and %s once each", got.Tickets, cut[0].ID, cut[1].ID)
	}
	if _, err := m.ReviewRequest("DEMO", id, ReviewPass, "his words hold", person); err == nil {
		t.Fatal("a review passed while every ticket was open")
	}
	movedTo(t, m, cut[0].ID, Done)
	movedTo(t, m, cut[1].ID, Dropped)
	standsAt(t, m, id, RequestOpen)
	if _, err := m.ReviewRequest("DEMO", id, ReviewPass, "his words hold", person); err == nil {
		t.Fatal("a review passed while a ticket was dropped")
	}
	movedTo(t, m, cut[1].ID, Done)
	standsAt(t, m, id, RequestUnreviewed)
	if _, err := m.ReviewRequest("DEMO", id, ReviewPass, "  ", person); err == nil {
		t.Fatal("a review with no note passed")
	}
	if _, err := m.ReviewRequest("DEMO", id, ReviewPass, "his words hold", agent); err == nil {
		t.Fatal("an agent that is not a manager passed a review")
	}
	if _, err := m.ReviewRequest("DEMO", id, ReviewFail, "A3 is not done", person); err != nil {
		t.Fatal(err)
	}
	standsAt(t, m, id, RequestRejected)
	if _, err := m.ReviewRequest("DEMO", id, ReviewPass, "A3 is done and his words hold", person); err != nil {
		t.Fatal(err)
	}
	standsAt(t, m, id, RequestDone)
	movedTo(t, m, cut[0].ID, Doing)
	standsAt(t, m, id, RequestOpen)
	movedTo(t, m, cut[0].ID, Done)
	standsAt(t, m, id, RequestUnreviewed)
	if _, err := m.ReviewRequest("DEMO", id, ReviewPass, "reopened and done again, his words hold", person); err != nil {
		t.Fatal(err)
	}
	standsAt(t, m, id, RequestDone)
	if _, err := m.AddToRequest("DEMO", id, []string{cut[2].ID}, person); err != nil {
		t.Fatal(err)
	}
	standsAt(t, m, id, RequestOpen)
	movedTo(t, m, cut[2].ID, Done)
	standsAt(t, m, id, RequestUnreviewed)
	listed, err := m.Store.RequestStandings("DEMO")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Request.ID != bare.Request.ID || listed[1].Request.ID != id {
		t.Fatalf("the list holds %d requests, want %s then %s", len(listed), bare.Request.ID, id)
	}
}
