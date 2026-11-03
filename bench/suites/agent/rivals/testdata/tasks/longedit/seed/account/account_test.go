package account

import (
	"errors"
	"testing"
)

func TestWithdrawStopsAtOverdraft(t *testing.T) {
	a := New("a1", "Ada")
	a, err := a.Withdraw(5000)
	if err != nil || a.Balance != -5000 {
		t.Fatalf("balance %d, err %v", a.Balance, err)
	}
	if _, err := a.Withdraw(1); !errors.Is(err, ErrOverdrawn) {
		t.Fatalf("err %v", err)
	}
}

func TestOverdrawnLists(t *testing.T) {
	accounts := []Acct{{ID: "a", Balance: -1}, {ID: "b", Balance: 3}}
	if over := Overdrawn(accounts); len(over) != 1 || over[0].ID != "a" {
		t.Fatalf("over %v", over)
	}
}
