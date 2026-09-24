package links

import (
	"strconv"

	"tofu/interface/tui/picker"
)

const (
	title      = "links"
	pickHint   = "↑↓ pick   enter copies   type to filter"
	emptyTitle = "this session carried no link"
	timesMark  = "×"
)

type Model struct {
	picker.Model[Link]
}

func (m *Model) Set(found []Link, trouble string) { m.Model.Set(look(), found, trouble) }

func look() picker.Look[Link] {
	return picker.Look[Link]{
		Title: title,
		One:   "link",
		Many:  "links",
		Hint:  pickHint,
		Empty: emptyTitle,
		Match: func(one Link) string { return one.URL + " " + one.From },
		Columns: []picker.Column[Link]{
			{Text: func(one Link) string { return one.URL }, Wide: true},
			{Text: func(one Link) string { return one.From }, Most: picker.Quarter},
			{Text: repeats},
		},
	}
}

func repeats(one Link) string {
	if one.Count < 2 {
		return ""
	}
	return timesMark + strconv.Itoa(one.Count)
}
