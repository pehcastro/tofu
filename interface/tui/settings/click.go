package settings

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
)

func (m *Model) Click(x, y int) Intent {
	switch {
	case m.dialog.open:
		return m.clickChoice(x, y)
	case m.search.open:
		m.clickResult(x, y)
		return Intent{}
	case m.number.open:
		return Intent{}
	}
	geo := m.layout()
	if x < geo.rail {
		index := y - categoriesTop
		if index >= 0 && index < len(m.categories()) && x >= panePad && x < geo.rail-panePad {
			m.category, m.cursor = index, 0
		}
		return Intent{}
	}
	left := geo.rail + panePad
	if x < left || y < rowsTop || (y-rowsTop)%m.rowHeight() != 0 {
		return Intent{}
	}
	indices := m.inCategory()
	start, end := m.visibleRows(len(indices))
	index := start + (y-rowsTop)/m.rowHeight()
	if index >= end {
		return Intent{}
	}
	row := m.Rows[indices[index]]
	line := ansi.Strip(strings.SplitN(look.SettingRow(geo.main-2*panePad, false, row.Label, row.Description, m.display(row)), "\n", 2)[0])
	labelEnd := markerCells + ansi.StringWidth(row.Label)
	control := ansi.StringWidth(strings.TrimRight(line, " "))
	controlStart := control - ansi.StringWidth(strings.TrimSpace(ansi.Cut(line, labelEnd, control)))
	at := x - left
	if (at >= markerCells && at < labelEnd) || (at >= controlStart && at < control) {
		m.cursor = index
		return m.activate()
	}
	return Intent{}
}

func (m *Model) clickChoice(x, y int) Intent {
	row, _ := m.selected()
	modal := m.choiceDialog()
	x0, y0 := centre(modal, m.width, m.height)
	lines := strings.Split(ansi.Strip(modal), "\n")
	if y < y0 || y >= y0+len(lines) {
		return Intent{}
	}
	line := lines[y-y0]
	label := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
	start, end := m.choiceWindow(row)
	for i := start; i < end; i++ {
		if row.Choices[i] == label && pointer.TextHit(line, label, x-x0) {
			m.dialog.cursor = i
			return m.commit(row)
		}
	}
	return Intent{}
}

func (m *Model) clickResult(x, y int) {
	found := m.matches()
	x0, y0 := centre(m.searchDialog(), m.width, m.height)
	start, end := m.searchWindow(found)
	index := start + y - y0 - searchResultsTop
	if index < start || index >= end {
		return
	}
	left := x0 + panePad + markerCells
	if x >= left && x < left+ansi.StringWidth(resultLabel(m.Rows[found[index]])) {
		m.search.cursor = index
		m.jump()
	}
}
