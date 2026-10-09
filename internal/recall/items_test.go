package recall

import (
	"reflect"
	"strings"
	"testing"
)

func TestItemsNameEveryPieceOfTheContextWithItsBandAndFate(t *testing.T) {
	cfg := Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 1}
	bands := Bands{Identity: 100, Facts: 100, WorkingSet: 1000, Recent: 50}
	fact := "fact: notes.txt :: read, 100 bytes"
	read := `read {"path":"a.go"}`
	c := Conversation{
		Instructions: "sys",
		Facts:        []string{fact},
		Entries: []Entry{
			{Step: 0, Text: "the task: do it", Said: "do it"},
			{Step: 1, Calls: "\n" + read},
			{Step: 1, Tool: "read", Call: "c1", SupersedeKey: read, Text: strings.Repeat("x", 300)},
			{Step: 2, Text: "looking"},
			{Step: 2, Tool: "bash", Call: "c2", SupersedeKey: `bash {"command":"go test"}`, Text: "ok"},
			{Step: 3, Tool: "read", Call: "c3", SupersedeKey: read, Text: strings.Repeat("y", 400)},
			{Step: 4, Text: "done"},
		},
	}
	framed := func(bytes int) int { return bytes + 40 }
	want := []Item{
		{Band: BandIdentity, Kind: ItemSystem, Name: "instructions", Tokens: 3, Fate: FateFixed},
		{Band: BandFacts, Kind: ItemFact, Name: "notes.txt", Tokens: len(fact), Fate: FateCarried},
		{Band: BandWorkingSet, Kind: ItemSaid, Name: "do it", Tokens: framed(15), Fate: FateCarried},
		{Band: BandWorkingSet, Kind: ItemMessage, Name: read, Tokens: framed(0) + len(read) + 1, Fate: FateDropped, Step: 1},
		{Band: BandWorkingSet, Kind: ItemResult, Name: "read a.go", Tokens: framed(300), Fate: FateDropped, Step: 1},
		{Band: BandWorkingSet, Kind: ItemMessage, Name: "looking", Tokens: framed(7), Fate: FateDropped, Step: 2},
		{Band: BandWorkingSet, Kind: ItemResult, Name: "bash go test", Tokens: framed(2), Fate: FateFact, Step: 2},
		{Band: BandWorkingSet, Kind: ItemResult, Name: "read a.go", Tokens: framed(400), Fate: FateFact, Step: 3},
		{Band: BandRecent, Kind: ItemMessage, Name: "done", Tokens: framed(4), Fate: FateHeld, Step: 4},
	}
	items := Items(cfg, bands, c)
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items\n got %+v\nwant %+v", items, want)
	}
	summed := map[Band]int{}
	for _, item := range items {
		summed[item.Band] += item.Tokens
	}
	measured := Measure(cfg, bands, c)
	if measured := (map[Band]int{BandIdentity: measured.Identity, BandFacts: measured.Facts, BandWorkingSet: measured.WorkingSet, BandRecent: measured.Recent}); !reflect.DeepEqual(summed, measured) {
		t.Fatalf("items add up to %v per band, the occupancy says %v", summed, measured)
	}
}
