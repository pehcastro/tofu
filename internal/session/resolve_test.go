package session

import (
	"errors"
	"io/fs"
	"testing"
	"time"
)

func named(t *testing.T, store *Store, id, name string, at time.Time) Header {
	t.Helper()
	if err := store.Write(Header{ID: id, Root: id, At: at, Task: "a task"}, []Event{stepEvent(t, 1)}); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
	header, err := store.SetName(id, name)
	if err != nil {
		t.Fatalf("name %s: %v", id, err)
	}
	return header
}

func TestANameResolvesToTheSessionAndSurvivesASecondRename(t *testing.T) {
	store := NewStore(t.TempDir())
	at := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	named(t, store, IDPrefix+"one", "first try", at)

	matched, err := store.Resolve("first-try")
	if err != nil {
		t.Fatalf("resolve first-try: %v", err)
	}
	if len(matched) != 1 || matched[0].ID != IDPrefix+"one" {
		t.Fatalf("first-try resolved to %d sessions, want the one", len(matched))
	}

	if _, err := store.SetName(IDPrefix+"one", "second try"); err != nil {
		t.Fatalf("rename again: %v", err)
	}
	header, err := store.Header(IDPrefix + "one")
	if err != nil {
		t.Fatalf("header after the second rename: %v", err)
	}
	if header.Name == nil || *header.Name != "second-try" {
		t.Fatalf("after two renames the name is %v, want second-try alone", header.Name)
	}
	if _, err := store.Resolve("first-try"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the old name still resolves, so the session carries two names: %v", err)
	}
}

func TestTwoSessionsSharingANameBothResolveNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	at := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	named(t, store, IDPrefix+"older", "the gate", at)
	named(t, store, IDPrefix+"newer", "the gate", at.Add(time.Hour))

	matched, err := store.Resolve("the-gate")
	if err != nil {
		t.Fatalf("resolve the-gate: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("the-gate resolved to %d sessions, want both rather than a pick", len(matched))
	}
	if matched[0].ID != IDPrefix+"newer" || matched[1].ID != IDPrefix+"older" {
		t.Fatalf("the two read %s then %s, want the newest first", matched[0].ID, matched[1].ID)
	}
	if matched[0].At.Equal(matched[1].At) {
		t.Fatal("the two carry the same time, so a person has nothing to tell them apart by")
	}
}

func TestANameThatIsAlsoAnIDResolvesToTheID(t *testing.T) {
	store := NewStore(t.TempDir())
	at := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	named(t, store, IDPrefix+"decoy", IDPrefix+"target", at)
	named(t, store, IDPrefix+"target", "the real one", at.Add(time.Hour))

	matched, err := store.Resolve(IDPrefix + "target")
	if err != nil {
		t.Fatalf("resolve %starget: %v", IDPrefix, err)
	}
	if len(matched) != 1 || matched[0].ID != IDPrefix+"target" {
		t.Fatalf("the handle resolved to %+v, want the session whose id it is", matched)
	}
}

func TestAnIDWithoutItsPrefixStillResolves(t *testing.T) {
	store := NewStore(t.TempDir())
	named(t, store, IDPrefix+"short", "a name", time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC))

	matched, err := store.Resolve("short")
	if err != nil {
		t.Fatalf("resolve short: %v", err)
	}
	if len(matched) != 1 || matched[0].ID != IDPrefix+"short" {
		t.Fatalf("short resolved to %+v, want the one session", matched)
	}
}
