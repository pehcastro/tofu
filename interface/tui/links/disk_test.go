package links

import (
	"strings"
	"testing"

	isession "tofu/internal/session"
	"tofu/internal/sys"
)

const repoRoot = "../../.."

func TestThePickerOverTheSessionsRecordedUnderThisRepository(t *testing.T) {
	listing, err := isession.OpenAt(sys.StateDir(repoRoot)).Listing()
	if err != nil {
		t.Skipf("this checkout has no session store to read: %v", err)
	}
	if len(listing.Sessions) == 0 {
		t.Skip("this checkout carries no recorded session")
	}
	store := isession.OpenAt(sys.StateDir(repoRoot))
	carrying, total := 0, 0
	var richest Model
	richest.SetSize(100, 16)
	best := 0
	for _, header := range listing.Sessions {
		talk, err := store.Conversation(header.ID)
		if err != nil {
			continue
		}
		found := Collect(talk)
		if len(found) == 0 {
			continue
		}
		carrying++
		total += len(found)
		if len(found) > best {
			best = len(found)
			richest.Set(found, "")
			t.Logf("%s carries %d links", header.ID, len(found))
		}
	}
	if carrying == 0 {
		t.Skipf("none of the %d recorded sessions carried a link", len(listing.Sessions))
	}
	drawn := richest.View()
	t.Logf("%d of %d sessions carried %d links in all\n%s", carrying, len(listing.Sessions), total, drawn)
	if !strings.Contains(drawn, "http") {
		t.Fatal("the picker drew no link over a session that carries one")
	}
}
