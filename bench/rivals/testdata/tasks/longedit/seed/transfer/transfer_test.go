package transfer

import (
	"testing"

	"example.com/ledger/account"
	"example.com/ledger/store"
)

func seeded() *store.Store {
	s := store.New()
	a := account.New("a1", "Ada")
	a.Balance = 10000
	s.Put(a)
	s.Put(account.New("b2", "Bo"))
	return s
}

func TestMoveBalances(t *testing.T) {
	s := seeded()
	if err := Move(s, "a1", "b2", 2500); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Get("a1")
	b, _ := s.Get("b2")
	if a.Balance != 7500 || b.Balance != 2500 {
		t.Fatalf("a %d, b %d", a.Balance, b.Balance)
	}
}

func TestMoveAllCollectsFailures(t *testing.T) {
	s := seeded()
	failed := MoveAll(s, []Transfer{{From: "a1", To: "b2", Cents: 100}, {From: "zz", To: "b2", Cents: 1}})
	if len(failed) != 1 {
		t.Fatalf("failed %v", failed)
	}
}

func TestExposure(t *testing.T) {
	s := seeded()
	got, err := Exposure(s, []Transfer{{From: "b2", To: "a1", Cents: 300}, {From: "a1", To: "b2", Cents: 100}})
	if err != nil || got != 300 {
		t.Fatalf("exposure %d, err %v", got, err)
	}
}
