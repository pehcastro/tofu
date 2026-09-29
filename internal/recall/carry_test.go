package recall_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"tofu/internal/recall"
	"tofu/internal/web"
)

func TestEachListingReadOnOneTabSurvivesAForkWithItsWholeUrlAndItsContent(t *testing.T) {
	filtered := "https://www.airbnb.com/s/Lisbon/homes?adults=4&min_bedrooms=3&amenities%5B%5D=7&checkin=2026-10-10&checkout=2026-10-17&price_max=400"
	filtered += strings.Repeat("&room_types%5B%5D=Entire%20home", (400-len(filtered))/31+1)
	filtered = filtered[:400]
	listings := []struct{ url, heading string }{
		{"https://www.airbnb.com/rooms/111", "Cozy loft by the river"},
		{"https://www.airbnb.com/rooms/222", "Sunny flat with a pool"},
		{filtered, "Stay near Lisbon"},
	}
	var entries []recall.Entry
	for step, listing := range listings {
		snapshot := fmt.Sprintf("tab 1 %s %q\n- main\n  - heading %q [level=1]\n  - button \"Reserve\" [ref=e3]", listing.url, listing.heading, listing.heading)
		entries = append(entries, recall.Entry{Step: step, Tool: "browser_observe", SupersedeKey: `browser_observe {"tab":1}`, Text: web.Untrusted("Chrome tab 1", snapshot)})
	}
	window, drops, err := recall.Compact(recall.NewStore(t.TempDir()), recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 1},
		recall.Bands{Recent: 1}, recall.Conversation{Entries: append(slices.Clone(entries), recall.Entry{Step: len(entries), Text: "the listings are read"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, drop := range drops {
		if drop.Reason == recall.DroppedSuperseded {
			t.Errorf("the observe at step %d was dropped as superseded by a later observe of another page:\n%s", drop.Step, window.Entries[drop.Step].Text)
		}
	}
	carry, err := recall.DistilledCarry(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if len(carry.Facts) != len(listings) {
		t.Fatalf("the carry holds %d fact lines, want %d:\n%s", len(carry.Facts), len(listings), strings.Join(carry.Facts, "\n"))
	}
	for i, listing := range listings {
		line := carry.Facts[i]
		if !strings.Contains(line, listing.url+" :: ") {
			t.Errorf("fact %d does not name its whole url:\n%s", i, line)
		}
		if !strings.Contains(line, listing.heading) {
			t.Errorf("fact %d holds no content line %q:\n%s", i, listing.heading, line)
		}
		if strings.Contains(line, "the text between") {
			t.Errorf("fact %d carries the untrusted wrapper:\n%s", i, line)
		}
	}
}

func TestAPageReadTwiceUnderAFreshRefFsidAndItsArtifactFetchKeyAsOnePage(t *testing.T) {
	page := "https://www.airbnb.com/s/Atibaia/homes?adults=4&place_id=ChIJ&ref_fsid="
	observe := func(step int, fsid string) recall.Entry {
		return recall.Entry{Step: step, Tool: "browser_observe", SupersedeKey: `browser_observe {"tab":1}`,
			Text: web.Untrusted("Chrome tab 1", "tab 1 "+page+fsid+" \"Atibaia\"\n- heading \"Houses in Atibaia\"")}
	}
	store := recall.NewStore(t.TempDir())
	first, _, err := recall.Distil(store, recall.Conversation{Entries: []recall.Entry{observe(0, "a1")}}, 120)
	if err != nil {
		t.Fatal(err)
	}
	handle := strings.TrimPrefix(strings.Split(strings.Split(first[0], ", artifact ")[1], ",")[0], " ")
	fetch := recall.Entry{Step: 2, Tool: "artifact_fetch", SupersedeKey: `artifact_fetch {"handle":"` + handle + `","offset":0,"length":400}`, Text: "tab 1 houses in Atibaia"}
	sheet, _, err := recall.Distil(store, recall.Conversation{Facts: first, Entries: []recall.Entry{observe(1, "b2"), fetch}}, 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheet) != 1 {
		t.Errorf("one page read under two ref_fsid values and fetched once holds %d fact lines, want 1:\n%s", len(sheet), strings.Join(sheet, "\n"))
	}
	if other, _, _ := recall.Distil(store, recall.Conversation{Entries: []recall.Entry{observe(0, "a1"), {Step: 1, Tool: "browser_observe", SupersedeKey: `browser_observe {"tab":1}`,
		Text: web.Untrusted("Chrome tab 1", "tab 1 "+strings.Replace(page, "ChIJ", "Other", 1)+"a1 \"Elsewhere\"")}}}, 120); len(other) != 2 {
		t.Errorf("two places told apart only by place_id key as one:\n%s", strings.Join(other, "\n"))
	}
}
