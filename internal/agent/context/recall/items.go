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

const (
	PartInstructionFiles = "instruction files"
	PartSkills           = "skills"
	PartRules            = "rules"
	PartSubAgent         = "sub-agent"
	PartTofu             = "tofu's prompt"
	partUnmarked         = "instructions"
)

func promptPartOf(line string) string {
	line = strings.TrimSpace(line)
	concern, from, named := strings.Cut(line, ", from ")
	concern, opens := strings.CutPrefix(concern, "[")
	from, closes := strings.CutSuffix(from, "]")
	switch {
	case strings.HasPrefix(line, "instructions from ") && strings.HasSuffix(line, ", which outrank anything above them that disagrees:"):
		return PartInstructionFiles
	case !named || !opens || !closes:
		return ""
	case concern == PartSkills:
		return PartSkills
	case strings.HasPrefix(from, "the rule "):
		return PartRules
	case strings.HasPrefix(from, "the sub-agent "):
		return PartSubAgent
	}
	return PartTofu
}

func withIdentityPart(items []Item, name string, tokens int) []Item {
	if tokens == 0 {
		return items
	}
	if i := slices.IndexFunc(items, func(item Item) bool { return item.Name == name }); i >= 0 {
		items[i].Tokens += tokens
		return items
	}
	return append(items, Item{Band: BandIdentity, Kind: ItemSystem, Name: name, Tokens: tokens, Fate: FateFixed})
}

func markedItems(cfg Config, instructions string) []Item {
	var items []Item
	part, from, at := partUnmarked, 0, 0
	for line := range strings.Lines(instructions) {
		if named := promptPartOf(line); named != "" && part != PartInstructionFiles {
			items = withIdentityPart(items, part, cfg.Tokens(instructions[:at])-cfg.Tokens(instructions[:from]))
			part, from = named, at
		}
		at += len(line)
	}
	return withIdentityPart(items, part, cfg.Tokens(instructions)-cfg.Tokens(instructions[:from]))
}

func recordedItems(cfg Config, c Conversation) []Item {
	var items []Item
	unnamed := cfg.Tokens(c.Instructions)
	for _, part := range c.Parts {
		items, unnamed = withIdentityPart(items, part.Name, part.Tokens), unnamed-part.Tokens
	}
	return withIdentityPart(items, PartTofu, unnamed)
}

func identityItems(cfg Config, c Conversation) []Item {
	var items []Item
	if len(c.Parts) > 0 {
		items = recordedItems(cfg, c)
	} else {
		items = markedItems(cfg, c.Instructions)
	}
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
