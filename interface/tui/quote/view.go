package quote

import (
	"tofu/interface/tui/picker"
	"tofu/interface/tui/trace"
)

const (
	title      = "quote"
	pickHint   = "↑↓ pick   enter writes the reference   type to filter"
	emptyTitle = "this session has nothing to quote yet"
)

type Model struct {
	picker.Model[Turn]
}

func (m *Model) Set(turns []Turn, trouble string) { m.Model.Set(look(), turns, trouble) }

func shortEvent(one Turn) string { return trace.Short(one.Event) }

func look() picker.Look[Turn] {
	return picker.Look[Turn]{
		Title: title,
		One:   "turn",
		Many:  "turns",
		Hint:  pickHint,
		Empty: emptyTitle,
		Match: func(one Turn) string { return one.Text + " " + one.From + " " + shortEvent(one) },
		Columns: []picker.Column[Turn]{
			{Text: shortEvent},
			{Text: func(one Turn) string { return one.From }, Most: picker.Quarter},
			{Text: func(one Turn) string { return one.Text }, Wide: true},
		},
	}
}
