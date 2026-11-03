package store

import (
	"testing"

	"example.com/ledger/account"
)

func TestPutGetList(t *testing.T) {
	s := New()
	s.Put(account.New("b2", "Bo", "EUR"))
	s.Put(account.New("a1", "Ada", "EUR"))
	got, err := s.Get("a1")
	if err != nil || got.Owner != "Ada" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := s.Get("zz"); err == nil {
		t.Fatal("found an account that was never put")
	}
	if list := s.List(); len(list) != 2 || list[0].ID != "a1" {
		t.Fatalf("list %+v", list)
	}
}
