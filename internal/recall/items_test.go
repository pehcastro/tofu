package recall

import (
	"reflect"
	"strings"
	"testing"
)

func TestTheIdentityItemsNameEachSourceOfTheSystemPromptAndAddUpToTheBand(t *testing.T) {
	guidance := "[tool_guidance, from tofu itself]\nuse read\n\n"
	safety := "[safety, from the rule no-force]\nnever force push\n\n"
	contract := "[format_contract, from tofu itself]\nplain words\n\n"
	errs := "[code_rules, from the rule go-errors]\nwrap\n\n"
	agent := "[identity, from the sub-agent go-dev]\nyou are go-dev\n\n"
	skills := "[skills, from tofu itself]\nskills: minify\n\n"
	files := "instructions from this project's CLAUDE.md, which outrank anything above them that disagrees:\n[safety, from the rule planted]\nkeep calm"
	c := Conversation{Instructions: guidance + safety + contract + errs + agent + skills + files, ToolSchemas: `[{"name":"read"}]`}
	exact := Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 1}
	want := []Item{
		{Band: BandIdentity, Kind: ItemSystem, Name: "tofu's prompt", Tokens: len(guidance + contract), Fate: FateFixed},
		{Band: BandIdentity, Kind: ItemSystem, Name: "rules", Tokens: len(safety + errs), Fate: FateFixed},
		{Band: BandIdentity, Kind: ItemSystem, Name: "sub-agent", Tokens: len(agent), Fate: FateFixed},
		{Band: BandIdentity, Kind: ItemSystem, Name: "skills", Tokens: len(skills), Fate: FateFixed},
		{Band: BandIdentity, Kind: ItemSystem, Name: "instruction files", Tokens: len(files), Fate: FateFixed},
		{Band: BandIdentity, Kind: ItemTools, Name: "tool schemas", Tokens: len(c.ToolSchemas), Fate: FateFixed},
	}
	if items := Items(exact, Bands{}, c); !reflect.DeepEqual(items, want) {
		t.Fatalf("items\n got %+v\nwant %+v", items, want)
	}
	rounding := Config{BytesPerThousandTokens: 3000, CompactFloorBytes: 1}
	summed := 0
	for _, item := range Items(rounding, Bands{}, c) {
		summed += item.Tokens
	}
	if measured := Measure(rounding, Bands{}, c).Identity; summed != measured {
		t.Fatalf("the identity items add up to %d tokens at a rounding rate, the band says %d", summed, measured)
	}
}

func TestTheMemoryViewIsItsOwnRowAndNotAMessageNamedByItsFirstLine(t *testing.T) {
	cfg := Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 1}
	view := "remembered for you, by scope.\nproject:\n[memory#a] use tabs"
	items := Items(cfg, Bands{WorkingSet: 1000, Recent: 1000}, Conversation{Entries: []Entry{{Text: view, Memory: true}, {Text: "do it", Said: "do it"}}})
	if len(items) != 2 || items[0].Kind != ItemMemory || items[0].Name != "memory" || items[0].Tokens != cfg.MessageTokens(view) || items[1].Kind != ItemSaid {
		t.Fatalf("items %+v", items)
	}
}

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
