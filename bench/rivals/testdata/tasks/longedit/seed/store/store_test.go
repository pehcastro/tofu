package store

import (
	"testing"

	"example.com/ledger/account"
)

func TestPutGetList(t *testing.T) {
	s := New()
	s.Put(account.New("b2", "Bo"))
	s.Put(account.New("a1", "Ada"))
	got, ok := s.Get("a1")
	if !ok || got.Owner != "Ada" {
		t.Fatalf("got %+v, %t", got, ok)
	}
	if _, ok := s.Get("zz"); ok {
		t.Fatal("found an account that was never put")
	}
	if list := s.List(); len(list) != 2 || list[0].ID != "a1" {
		t.Fatalf("list %+v", list)
	}
}
