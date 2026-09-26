package palette

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
)

const (
	searchMaxRows     = 8
	searchChrome      = 9
	searchLabelMargin = 2
	searchLabelMin    = 8
	searchFilterMin   = 16
)

type Result struct {
	Label, Detail, Reference string
	Screen                   int
	Section, Row             int
}

type SearchChoice struct {
	Result                  Result
	Done, Cancelled, Inside bool
}

type Search struct {
	title, hint string
	list        list[Result]
}

func NewSearch(title, hint string, find func(query string) []Result) Search {
	return Search{title: title, hint: hint, list: newList(find)}
}

func (s *Search) Key(msg tea.KeyPressMsg) (SearchChoice, tea.Cmd) {
	result, done, cancelled, cmd := s.list.key(msg)
	return SearchChoice{Result: result, Done: done, Cancelled: cancelled}, cmd
}

func (s *Search) Paste(msg tea.PasteMsg) tea.Cmd { return s.list.edit(msg) }

func (s *Search) Click(x, y, width, height int) SearchChoice {
	modal, start, labels := s.modal(width, height)
	x0, y0 := place(modal, width, height, paneMargin)
	index, inside := rowAt(modal, x-x0, y-y0, 1, labels)
	if index < 0 {
		return SearchChoice{Inside: inside}
	}
	s.list.cursor = start + index
	return SearchChoice{Result: s.list.shown[s.list.cursor], Done: true, Inside: true}
}

func (s Search) Over(base string, width, height int) string {
	modal, _, _ := s.modal(width, height)
	x, y := place(modal, width, height, paneMargin)
	return compose(base, modal, x, y)
}

func (s Search) modal(width, height int) (modal string, start int, labels []string) {
	count := len(s.list.shown)
	modalWidth := min(dialogMaxWidth, width-dialogInset)
	visible := min(max(1, count), max(1, min(searchMaxRows, height-listChrome)))
	inner := modalWidth - 2*dialogPadding
	s.list.input.SetWidth(max(searchFilterMin, inner-filterInset))
	content := heading(s.title, look.Faint(s.hint), inner) + s.list.input.View() + "\n\n"
	start, end := look.VisibleRows(count, s.list.cursor, visible)
	for i := start; i < end; i++ {
		label := ansi.Truncate(s.list.shown[i].Label, max(searchLabelMin, inner-searchLabelMargin), "…")
		content += look.CatalogRow(inner, i == s.list.cursor, label) + "\n"
		labels = append(labels, label)
	}
	if count == 0 {
		content += look.Muted("No results")
	} else {
		content += "\n" + look.Faint(fmt.Sprintf("%d of %d results", s.list.cursor+1, count))
	}
	return look.ModalPane(modalWidth, min(height-paneHeightInset, visible+searchChrome), look.Panel, dialogPadding, content), start, labels
}
