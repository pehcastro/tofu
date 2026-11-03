package boardy

import (
	"testing"
)

func TestHandCarriesTheSectionsOrIsRefused(t *testing.T) {
	m := managedBoard(t, "DEMO", "OPS")
	if _, err := m.Store.Init("FREE", "FREE"); err != nil {
		t.Fatal(err)
	}
	bare, err := m.Store.Create("DEMO", Ticket{Front: Front{Title: "made before the sections"}, Problem: "p", Scope: "s", Acceptance: []string{"- a line"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"OPS", "FREE"} {
		_, err := m.Hand(bare.ID, to, m.Store, manager)
		refused(t, err, "Still works")
		if old, _ := m.Store.Get(bare.ID); old.Status == Dropped {
			t.Fatalf("a hand to %s refused for its sections still dropped %s", to, bare.ID)
		}
	}
	whole, err := m.Create("DEMO", ticketWith("made whole"), manager)
	if err != nil {
		t.Fatal(err)
	}
	landed, err := m.Hand(whole.ID, "OPS", m.Store, manager)
	if err != nil || landed.Before != "b" || landed.StillWorks != "w" || landed.ErrorCase != "e" {
		t.Fatalf("the handed ticket landed as %+v, %v, want its sections", landed, err)
	}
	stored, err := m.Store.Get(landed.ID)
	if err != nil || stored.Driven != "d" || stored.Feasibility != "f" {
		t.Fatalf("the handed ticket reads back as %+v, %v", stored, err)
	}
}
