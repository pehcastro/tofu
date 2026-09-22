package quote

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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

func TestThePickerOverTheRichestSessionRecordedUnderThisRepository(t *testing.T) {
	store, listing := recorded(t)
	var richest Model
	richest.SetSize(100, 18)
	best, chosen := 0, ""
	fromSteps, fromMessages := 0, 0
	for _, header := range listing.Sessions {
		talk, err := store.Conversation(header.ID)
		if err != nil {
			continue
		}
		turns := Collect(talk)
		if len(turns) == 0 {
			continue
		}
		if talk.FromSteps {
			fromSteps++
		} else {
			fromMessages++
		}
		if len(turns) > best {
			best, chosen = len(turns), header.ID
			richest.Set(turns, "")
		}
	}
	if best == 0 {
		t.Skipf("none of the %d recorded sessions held a quotable turn", len(listing.Sessions))
	}
	screen := ansi.Strip(richest.View())
	t.Logf("%d sessions read from steps and %d from messages, of %d recorded; the richest is %s with %d turns\n%s",
		fromSteps, fromMessages, len(listing.Sessions), chosen, best, screen)
	if !strings.Contains(screen, "#") {
		t.Fatalf("the picker drew no shortened id over %s\n%s", chosen, screen)
	}
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
