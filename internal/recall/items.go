package recall

import (
	"cmp"
	"slices"
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
	ItemMemory  ItemKind = "memory"
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

const instructionFiles = "instruction files"

func promptPartOf(line string) string {
	line = strings.TrimSpace(line)
	concern, from, named := strings.Cut(line, ", from ")
	concern, opens := strings.CutPrefix(concern, "[")
	from, closes := strings.CutSuffix(from, "]")
	switch {
	case strings.HasPrefix(line, "instructions from ") && strings.HasSuffix(line, ", which outrank anything above them that disagrees:"):
		return instructionFiles
	case !named || !opens || !closes:
		return ""
	case concern == "skills":
		return "skills"
	case strings.HasPrefix(from, "the rule "):
		return "rules"
	case strings.HasPrefix(from, "the sub-agent "):
		return "sub-agent"
	}
	return "tofu's prompt"
}

func identityItems(cfg Config, c Conversation) []Item {
	var items []Item
	part, from, at := "instructions", 0, 0
	addPart := func(to int) {
		if to <= from {
			return
		}
		tokens := cfg.Tokens(c.Instructions[:to]) - cfg.Tokens(c.Instructions[:from])
		if i := slices.IndexFunc(items, func(item Item) bool { return item.Name == part }); i >= 0 {
			items[i].Tokens += tokens
			return
		}
		items = append(items, Item{Band: BandIdentity, Kind: ItemSystem, Name: part, Tokens: tokens, Fate: FateFixed})
	}
	for line := range strings.Lines(c.Instructions) {
		if named := promptPartOf(line); named != "" && part != instructionFiles {
			addPart(at)
			part, from = named, at
		}
		at += len(line)
	}
	addPart(len(c.Instructions))
	if c.ToolSchemas != "" {
		items = append(items, Item{Band: BandIdentity, Kind: ItemTools, Name: "tool schemas", Tokens: cfg.Tokens(c.ToolSchemas), Fate: FateFixed})
	}
	return items
}

func Items(cfg Config, bands Bands, c Conversation) []Item {
	items := identityItems(cfg, c)
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
		case entry.Memory:
			item.Kind, item.Name = ItemMemory, "memory"
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
