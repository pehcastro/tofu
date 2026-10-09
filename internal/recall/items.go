package recall

import (
	"cmp"
	"strings"

	"tofu/internal/konst"
)

type Band string

const (
	BandIdentity   Band = "identity"
	BandFacts      Band = "facts"
	BandWorkingSet Band = "working_set"
	BandRecent     Band = "recent"
)

type ItemKind string

const (
	ItemSystem  ItemKind = "system"
	ItemTools   ItemKind = "tools"
	ItemFact    ItemKind = "fact"
	ItemSaid    ItemKind = "said"
	ItemMessage ItemKind = "message"
	ItemResult  ItemKind = "result"
)

type ItemFate string

const (
	FateFixed   ItemFate = "fixed"
	FateCarried ItemFate = "carried"
	FateFact    ItemFate = "fact"
	FateHeld    ItemFate = "held"
	FateDropped ItemFate = "dropped"
)

type Item struct {
	Band   Band     `json:"band"`
	Kind   ItemKind `json:"kind"`
	Name   string   `json:"name"`
	Tokens int      `json:"tokens"`
	Fate   ItemFate `json:"fate"`
	Step   int      `json:"step"`
}

func Items(cfg Config, bands Bands, c Conversation) []Item {
	var items []Item
	if c.Instructions != "" {
		items = append(items, Item{Band: BandIdentity, Kind: ItemSystem, Name: "instructions", Tokens: cfg.Tokens(c.Instructions), Fate: FateFixed})
	}
	if c.ToolSchemas != "" {
		items = append(items, Item{Band: BandIdentity, Kind: ItemTools, Name: "tool schemas", Tokens: cfg.Tokens(c.ToolSchemas), Fate: FateFixed})
	}
	for _, fact := range c.Facts {
		items = append(items, Item{Band: BandFacts, Kind: ItemFact, Name: factSource(fact), Tokens: cfg.Tokens(fact), Fate: FateCarried})
	}
	type carriedCall struct {
		tool, call string
		bytes      int
	}
	_, kept, _ := Distil(nil, c, konst.CarrySignpostBytes)
	becomesFact := map[carriedCall]bool{}
	for _, result := range kept {
		becomesFact[carriedCall{result.Tool, result.Call, result.Bytes}] = true
	}
	recent := recentFrom(cfg, bands, c.Entries)
	for i, entry := range c.Entries {
		item := Item{Band: BandWorkingSet, Kind: ItemMessage, Name: oneLine(cmp.Or(strings.TrimSpace(entry.Text), entry.Calls), konst.CarrySignpostBytes),
			Tokens: entry.tokens(cfg), Fate: FateDropped, Step: entry.Step}
		switch {
		case entry.Tool != "":
			item.Kind, item.Name = ItemResult, entry.Tool+" "+sourceOf(entry, nil)
			if becomesFact[carriedCall{entry.Tool, entry.Call, len(entry.Text)}] {
				item.Fate = FateFact
			}
		case entry.Said != "":
			item.Kind, item.Name, item.Fate = ItemSaid, oneLine(entry.Said, konst.CarrySignpostBytes), FateCarried
		}
		if i >= recent {
			item.Band, item.Fate = BandRecent, FateHeld
		}
		items = append(items, item)
	}
	return items
}
