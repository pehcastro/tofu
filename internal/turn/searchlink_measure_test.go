package turn

import (
	"strings"
	"testing"

	"tofu/internal/session"
)

func TestSearchLinksOverTheRecordedSessions(t *testing.T) {
	store := session.NewStore(recordedSessionsDir(t))
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	searches, hits, unoffered, none := 0, 0, 0, 0
	distinctTasks := map[string]bool{}
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			t.Fatalf("Body(%s): %v", header.ID, err)
		}
		conversation, err := ConversationFrom(events)
		if err != nil {
			t.Fatalf("ConversationFrom(%s): %v", header.ID, err)
		}
		for _, link := range SearchLinksOf(conversation) {
			searches++
			switch link.Kind {
			case SearchLinkHit:
				hits++
				distinctTasks[strings.TrimSpace(header.Task)] = true
			case SearchLinkUnoffered:
				unoffered++
			case SearchLinkNone:
				none++
			}
		}
	}
	t.Logf("%d sessions read, %d search calls found, %d hit, %d unoffered, %d none: "+
		"%d questions this join would have produced, %d of those with a distinct task, against the 12 TOFU-501 says are needed",
		len(listing.Sessions), searches, hits, unoffered, none, hits, len(distinctTasks))
}
