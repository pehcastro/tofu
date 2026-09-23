package sift

import (
	"fmt"

	"tofu/internal/web"
)

func PageCheap(unit PageUnit) Mark {
	if unit.Held != NotHeld {
		return Mark{Keep: true, Reason: string(unit.Held)}
	}
	if web.LinkOnly(unit.Kind, unit.Text) {
		return Mark{Reason: "a list item or row that is only a link"}
	}
	return Mark{Keep: true}
}

type PageUnitState struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

type PageState struct {
	Task  string          `json:"task"`
	URL   string          `json:"url"`
	Units []PageUnitState `json:"units"`
}

func BuildPageState(units []PageUnit, task, pageURL string) PageState {
	out := make([]PageUnitState, len(units))
	for i, unit := range units {
		out[i] = PageUnitState{Index: unit.Index, Kind: kindName(unit.Kind), Text: unit.Text}
	}
	return PageState{Task: task, URL: pageURL, Units: out}
}

func kindName(k web.UnitKind) string {
	switch k {
	case web.UnitHeading:
		return "heading"
	case web.UnitCode:
		return "code"
	case web.UnitItem:
		return "item"
	case web.UnitQuote:
		return "quote"
	case web.UnitRow:
		return "row"
	default:
		return "paragraph"
	}
}

func PageQuestionID(index int) string {
	return fmt.Sprintf("%s_%d", NeededQuestion, index)
}

func DecidePage(unit PageUnit, score float64, answered bool, keepAt float64) (Mark, error) {
	if unit.Held != NotHeld {
		return Mark{Keep: true, Reason: string(unit.Held)}, nil
	}
	if !answered {
		return Mark{}, fmt.Errorf("sift: unit %d carries no %q answer, so it cannot be judged", unit.Index, NeededQuestion)
	}
	if score >= keepAt {
		return Mark{Keep: true, Reason: fmt.Sprintf("still needed %.2f", score)}, nil
	}
	return Mark{Reason: fmt.Sprintf("still needed %.2f, under %.2f", score, keepAt)}, nil
}
