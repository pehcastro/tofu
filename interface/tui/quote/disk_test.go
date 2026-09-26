package quote

import (
	"testing"

	isession "tofu/internal/session"
	"tofu/internal/sys"
)

const repoRoot = "../../.."

func recorded(t *testing.T) (*isession.Store, isession.Listing) {
	t.Helper()
	store := isession.OpenAt(sys.StateDir(repoRoot))
	listing, err := store.Listing()
	if err != nil {
		t.Skipf("this checkout has no session store to read: %v", err)
	}
	if len(listing.Sessions) == 0 {
		t.Skip("this checkout carries no recorded session")
	}
	return store, listing
}

func TestASessionOnDiskCarryingOnlyStepEventsIsQuotableAndItsIDsResolve(t *testing.T) {
	store, listing := recorded(t)
	stepOnly := 0
	for _, header := range listing.Sessions {
		talk, err := store.Conversation(header.ID)
		if err != nil || !talk.FromSteps {
			continue
		}
		turns := Collect(talk)
		if len(turns) == 0 {
			continue
		}
		stepOnly++
		for _, one := range turns {
			back, err := Resolve(turns, hashOf(t, Ref(one.Event)))
			if err != nil {
				t.Fatalf("%s: the reference for %s does not resolve: %v", header.ID, one.Event, err)
			}
			if back.Event != one.Event {
				t.Fatalf("%s: a reference resolved to %s, want %s", header.ID, back.Event, one.Event)
			}
		}
	}
	if stepOnly == 0 {
		t.Skipf("none of the %d recorded sessions carries only step events", len(listing.Sessions))
	}
	t.Logf("%d of %d recorded sessions carry only step events, and every turn of each resolves", stepOnly, len(listing.Sessions))
}
