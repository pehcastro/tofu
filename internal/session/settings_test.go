package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTheDefaultLifetimeIsNeverAndNeverExpiresAnything(t *testing.T) {
	settings := DefaultSettings()
	if settings.Lifetime != LifetimeNever {
		t.Fatalf("the default lifetime is %s, want never", settings.Lifetime)
	}
	if settings.Lifetime.String() != "never" {
		t.Fatalf("never prints as %q", settings.Lifetime.String())
	}
	long := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if settings.Lifetime.Expired(long, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("a session ten years old expired under the default lifetime")
	}
}

func TestASessionOlderThanTheLifetimeIsExpiredAndTheFileIsStillThere(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	old := Header{ID: "turn-old", Root: "turn-old", At: now.Add(-40 * lifetimeDay)}
	young := Header{ID: "turn-young", Root: "turn-young", At: now.Add(-3 * lifetimeDay)}
	for _, header := range []Header{old, young} {
		if err := store.Write(header, nil); err != nil {
			t.Fatalf("write %s: %v", header.ID, err)
		}
	}

	lifetime, err := ParseLifetime("30d")
	if err != nil {
		t.Fatalf("parse 30d: %v", err)
	}
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	expired := map[string]bool{}
	for _, header := range listing.Sessions {
		expired[header.ID] = lifetime.Expired(header.LastAt(), now)
	}
	if !expired["turn-old"] {
		t.Error("a session 40 days old is not expired under a 30 day lifetime")
	}
	if expired["turn-young"] {
		t.Error("a session 3 days old is expired under a 30 day lifetime")
	}
	for _, id := range []string{"turn-old", "turn-young"} {
		if _, err := os.Stat(filepath.Join(store.dir, id, headerName)); err != nil {
			t.Fatalf("%s was deleted: %v", id, err)
		}
	}
	t.Logf("expired: %v, both still on disk", expired)
}

func TestALifetimeIsThirtySixtyNinetyOrNeverAndAnythingElseIsRefused(t *testing.T) {
	for _, text := range []string{"30d", "60d", "90d", "never", "  NEVER ", "45"} {
		if _, err := ParseLifetime(text); err != nil {
			t.Errorf("%q is refused: %v", text, err)
		}
	}
	for _, text := range []string{"", "forever", "-1d", "thirty"} {
		if _, err := ParseLifetime(text); err == nil {
			t.Errorf("%q was taken as a lifetime", text)
		}
	}
}

func TestASessionEndedByNewIsMarkedEndedAndAnOpenOneIsNotAndItStillReads(t *testing.T) {
	store := NewStore(t.TempDir())
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"turn-ended", "turn-open"} {
		if err := store.Write(Header{ID: id, Root: id, At: at}, nil); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}

	ended, err := store.End("turn-ended", EndedByNew, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("end turn-ended: %v", err)
	}
	if !ended.Ended() || ended.EndReason != EndedByNew {
		t.Fatalf("turn-ended reads back as %+v", ended)
	}
	if !ended.LastAt().Equal(at.Add(time.Hour)) {
		t.Errorf("an ended session was last at %s, want the end time", ended.LastAt())
	}
	reread, err := store.Header("turn-ended")
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if !reread.Ended() {
		t.Fatal("the end was not written to the header")
	}
	open, err := store.Header("turn-open")
	if err != nil {
		t.Fatalf("header turn-open: %v", err)
	}
	if open.Ended() {
		t.Fatalf("a session nobody ended is marked ended: %+v", open)
	}
	if _, err := store.End("turn-open", "abandoned", at); err == nil {
		t.Fatal("abandoned was taken as a reason a session ends")
	}
}
