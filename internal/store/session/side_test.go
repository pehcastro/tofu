package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestASideChatNeverTakesTheHead(t *testing.T) {
	start := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		headSet bool
		written func(store *Store, lead Header) string
	}{
		{"a side chat", true, func(store *Store, lead Header) string {
			return opened(t, store, Header{At: start.Add(time.Hour), Kind: KindSide, BranchedFrom: &Carried{Session: lead.ID}}).ID
		}},
		{"a fork of a side chat, which carries no kind", true, func(store *Store, lead Header) string {
			side := opened(t, store, Header{At: start.Add(time.Hour), Kind: KindSide, BranchedFrom: &Carried{Session: lead.ID}})
			return opened(t, store, Header{At: start.Add(2 * time.Hour), CarriedFrom: &Carried{Session: side.ID}}).ID
		}},
		{"no HEAD file and a newer side chat", false, func(store *Store, lead Header) string {
			side := opened(t, store, Header{At: start.Add(time.Hour), Kind: KindSide, BranchedFrom: &Carried{Session: lead.ID}})
			return opened(t, store, Header{At: start.Add(2 * time.Hour), CarriedFrom: &Carried{Session: side.ID}}).ID
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := OpenAt(t.TempDir())
			lead := opened(t, store, Header{At: start})
			if c.headSet {
				if err := store.SetHead(lead.ID); err != nil {
					t.Fatal(err)
				}
			}
			side := c.written(store, lead)
			if c.headSet {
				if err := store.SetHead(side); err != nil {
					t.Fatal(err)
				}
			}
			head, err := store.Head()
			if err != nil || head.ID != lead.ID {
				t.Fatalf("the head is %q (%v), want the lead %s and never the side chat %s", head.ID, err, lead.ID, side)
			}
		})
	}
}

func TestAnUnreadableHeaderIsNotTakenForAMainSession(t *testing.T) {
	store := OpenAt(t.TempDir())
	side := opened(t, store, Header{Kind: KindSide, BranchedFrom: &Carried{Session: "lead"}})
	if err := os.WriteFile(filepath.Join(store.Dir(side.ID), headerName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Side(side.ID); err == nil {
		t.Fatal("a side chat whose header does not parse read as no side chat, so its turn would run with every tool")
	}
	if err := store.SetHead(side.ID); err == nil {
		t.Fatal("the head moved to a session whose kind could not be read")
	}
}
