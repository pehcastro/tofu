package api

import (
	"errors"
	"testing"

	"example.com/ledger/account"
	"example.com/ledger/store"
)

func TestOpenTwice(t *testing.T) {
	sv := Service{Store: store.New()}
	if _, err := sv.Open("a1", "Ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.Open("a1", "Ada"); !errors.Is(err, ErrExists) {
		t.Fatalf("err %v", err)
	}
}

func TestFreezeBlocksDeposit(t *testing.T) {
	sv := Service{Store: store.New()}
	if _, err := sv.Open("a1", "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := sv.Freeze("a1"); err != nil {
		t.Fatal(err)
	}
	a, _ := sv.Store.Get("a1")
	if _, err := a.Deposit(1); !errors.Is(err, account.ErrFrozen) {
		t.Fatalf("err %v", err)
	}
	if err := sv.Thaw("zz"); err == nil {
		t.Fatal("thawed an unknown account")
	}
}
