package sift

import (
	"fmt"

	"tofu/internal/web"
)

type PageUnit struct {
	Index int
	Kind  web.UnitKind
	Text  string
	Held  Held
}

func SplitPage(units []web.Unit) []PageUnit {
	out := make([]PageUnit, len(units))
	for i, unit := range units {
		held := NotHeld
		if unit.Kind == web.UnitCode {
			held = HeldCode
		}
		out[i] = PageUnit{Index: i, Kind: unit.Kind, Text: unit.Text, Held: held}
	}
	return out
}

func JoinPageUnits(units []PageUnit) string {
	rendered := make([]web.Unit, len(units))
	for i, unit := range units {
		rendered[i] = web.Unit{Kind: unit.Kind, Text: unit.Text}
	}
	return web.Render(rendered)
}

func PageMessage(units []PageUnit, marks []Mark, mode Mode) string {
	if mode == ModeShadow {
		return JoinPageUnits(units)
	}
	kept, keptBytes, total := 0, 0, 0
	rendered := make([]web.Unit, len(units))
	for i, unit := range units {
		total += len(unit.Text)
		if marks[i].Keep {
			kept++
			keptBytes += len(unit.Text)
			rendered[i] = web.Unit{Kind: unit.Kind, Text: unit.Text}
			continue
		}
		rendered[i] = web.Unit{Kind: unit.Kind, Text: fmt.Sprintf("[sift: %d bytes elided, %s]", len(unit.Text), marks[i].Reason)}
	}
	out := web.Render(rendered)
	if kept == len(units) {
		return out
	}
	return out + fmt.Sprintf("\n\n[sift: kept %d of %d units, %d of %d bytes]\n", kept, len(units), keptBytes, total)
}
