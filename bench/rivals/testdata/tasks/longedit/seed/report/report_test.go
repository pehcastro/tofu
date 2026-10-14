package report

import (
	"testing"

	"example.com/ledger/account"
	"example.com/ledger/store"
)

func TestStatement(t *testing.T) {
	s := store.New()
	a := account.New("a1", "Ada")
	a.Balance = 1250
	s.Put(a)
	got, err := Statement(s, "a1")
	if err != nil || got != "Ada (a1): 12.50" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if _, err := Statement(s, "zz"); err == nil {
		t.Fatal("a statement for an unknown account")
	}
}

func TestMoneyNegative(t *testing.T) {
	if got := Money(-705); got != "-7.05" {
		t.Fatalf("got %q", got)
	}
}
