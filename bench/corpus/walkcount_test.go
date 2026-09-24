package corpus

import (
	"os"
	"testing"

	"tofu/internal/session"
)

const (
	sessionsOnDiskAtTOFU589 = 111
	eventShapedAtTOFU589    = 53
	turnsWalkedAtTOFU589    = 118
)

func TestWalkSessionsStillReachesEverySessionTheStoreLists(t *testing.T) {
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("no %s on this machine: %v", sessionsDir, err)
	}
	store := session.NewStore(sessionsDir)
	listing, err := store.Listing()
	if err != nil {
		t.Fatal(err)
	}
	eventShaped := 0
	for _, header := range listing.Sessions {
		if store.Shape(header.ID) == session.ShapeEvents {
			eventShaped++
		}
	}
	walked, err := WalkSessions(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("store lists %d sessions, %d of them event shaped; WalkSessions read %d turns from %d entries and skipped %d",
		len(listing.Sessions), eventShaped, len(walked.Turns), walked.EntryCount, len(walked.Skipped))
	if len(listing.Sessions) < sessionsOnDiskAtTOFU589 || eventShaped < eventShapedAtTOFU589 {
		t.Fatalf("the corpus lost sessions: %d listed and %d event shaped, and TOFU-589 measured %d and %d",
			len(listing.Sessions), eventShaped, sessionsOnDiskAtTOFU589, eventShapedAtTOFU589)
	}
	if len(walked.Turns) < turnsWalkedAtTOFU589 {
		t.Fatalf("WalkSessions read %d turns, and it read %d before the reader stopped spelling the body filename", len(walked.Turns), turnsWalkedAtTOFU589)
	}
	skipped := map[string]string{}
	for _, skip := range walked.Skipped {
		skipped[skip.Path] = skip.Reason
	}
	for _, header := range listing.Sessions {
		for _, name := range []string{header.ID, header.ID + ".json"} {
			if reason, gone := skipped[name]; gone {
				t.Fatalf("%s is a session the store lists and WalkSessions skipped it: %s", name, reason)
			}
		}
	}
}
