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

func TestAForkCarriesTheLastInteractiveSnapshotOfEachTabWithItsUrl(t *testing.T) {
	snapshot := func(step int, tool, args, page string) recall.Entry {
		return recall.Entry{Step: step, Tool: tool, SupersedeKey: tool + " " + args, Text: web.Untrusted("Chrome tab", page)}
	}
	entries := []recall.Entry{
		snapshot(0, "browser_observe", `{"tab":1}`, "tab 1 https://www.airbnb.com/s/Atibaia/homes \"Search\"\n- button \"Filters\" [ref=e1]"),
		snapshot(1, "browser_observe", `{"tab":2}`, "tab 2 https://www.airbnb.com/rooms/222 \"Sunny flat\"\n- button \"Reserve\" [ref=e7]"),
		snapshot(2, "browser_act", `{"tab":1,"actions":[{"action":"click","ref":"e1"}]}`, "tab 1 https://www.airbnb.com/s/Atibaia/homes?adults=4 \"Filters\"\n- dialog \"Filters\"\n  - button \"Show 27 places\" [ref=e9]"),
		snapshot(3, "browser_observe", `{"tab":1,"interactive":false}`, "tab 1 https://www.airbnb.com/s/Atibaia/homes?adults=4 \"Filters\"\n- heading \"the whole tree, too long to carry\""),
	}
	carry, err := recall.DistilledCarry(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	_, snapshots, _ := strings.Cut(carry.Text, "the last snapshot of each tab")
	for _, want := range []string{
		"tab 1 https://www.airbnb.com/s/Atibaia/homes?adults=4 \"Filters\"\n- dialog \"Filters\"\n  - button \"Show 27 places\" [ref=e9]",
		"tab 2 https://www.airbnb.com/rooms/222 \"Sunny flat\"\n- button \"Reserve\" [ref=e7]",
		"came from Chrome tab",
	} {
		if !strings.Contains(snapshots, want) {
			t.Errorf("the carry's snapshots do not hold %q:\n%s", want, carry.Text)
		}
	}
	for _, stale := range []string{"button \"Filters\" [ref=e1]", "the whole tree, too long to carry"} {
		if strings.Contains(snapshots, stale) {
			t.Errorf("the carry's snapshots hold %q, which is not the last interactive snapshot of its tab:\n%s", stale, carry.Text)
		}
	}
}

func TestACarryBesideAHeldTailRepeatsNothingTheTailHoldsAndKeepsWhatItDropped(t *testing.T) {
	page := func(step int, tool, args, body string) recall.Entry {
		return recall.Entry{Step: step, Tool: tool, SupersedeKey: tool + " " + args, Text: web.Untrusted("Chrome tab", body)}
	}
	entries := []recall.Entry{
		page(0, "browser_observe", `{"tab":2}`, "tab 2 https://stays.test/rooms/2 \"Two\"\n- button \"Reserve two\" [ref=e7]"),
		page(1, "browser_observe", `{"tab":1}`, "tab 1 https://stays.test/s \"Search\"\n- button \"Filters\" [ref=e1]"),
		{Step: 2, Text: "opening the filters"},
		{Step: 2, Tool: "browser_act", SupersedeKey: `browser_act {"tab":1,"actions":[{"action":"click","ref":"e1"}]}`, Text: "1. click e1: a dialog opened\nran 1 of 1\n\n" + web.Untrusted("Chrome tab", "a dialog")},
		page(3, "browser_observe", `{"tab":1}`, "tab 1 https://stays.test/s?filters=1 \"Filters\"\n- button \"Show 27\" [ref=e9]"),
		{Step: 4, Text: "the filters are open"},
	}
	whole, err := recall.DistilledCarry(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	beside, err := recall.DistilledCarry(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: entries, HeldWhole: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, held := range []string{"the last thing it said or did:\nthe filters are open", "the last browser actions it took", "tab 1 https://stays.test/s?filters=1 \"Filters\"\n"} {
		if !strings.Contains(whole.Text, held) || strings.Contains(beside.Text, held) {
			t.Errorf("%q is in the carry with no tail %v and beside the tail that holds it %v", held, strings.Contains(whole.Text, held), strings.Contains(beside.Text, held))
		}
	}
	for _, kept := range []string{"\n- button \"Reserve two\" [ref=e7]", "fact: https://stays.test/s?filters=1 :: browser_observe"} {
		if !strings.Contains(beside.Text, kept) {
			t.Errorf("the carry beside the tail lost %q, which only the dropped history held or which the fact sheet indexes:\n%s", kept, beside.Text)
		}
	}
	if strings.Contains(beside.Text, "\n- button \"Filters\" [ref=e1]") {
		t.Errorf("the carry holds tab 1's older snapshot when its newest is in the tail:\n%s", beside.Text)
	}
}

func TestAPageReadTwiceUnderAFreshRefFsidAndItsArtifactFetchKeyAsOnePage(t *testing.T) {
	page := "https://www.airbnb.com/s/Atibaia/homes?adults=4&place_id=ChIJ&ref_fsid="
	observe := func(step int, fsid string) recall.Entry {
		return recall.Entry{Step: step, Tool: "browser_observe", SupersedeKey: `browser_observe {"tab":1}`, Handle: "held-" + fsid,
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
