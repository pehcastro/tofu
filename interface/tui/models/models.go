package models

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/theme"
	library "tofu/internal/llm/models"
	"tofu/internal/widget"
)

const (
	minimumWidth = 20
	gap          = "  "
	pickedMark   = "› "
	plainMark    = "  "
	title        = "models"
	pickHint     = "↑↓ pick a model"
	emptyTitle   = "the library has no model to pick"
)

type Row struct {
	Slug   string
	Use    library.Use
	Window string
	Reason string
}

func (r Row) excluded() bool { return r.Use == library.UseExcluded }

type Group struct {
	Source string
	Rows   []Row
}

type Model struct {
	Groups []Group
	pick   int
	width  int
	height int
}

func Slug(model library.Model) string {
	return string(model.Subscription) + "/" + model.ID
}

func Build(loaded library.Library) Model {
	order := make([]string, 0, len(loaded.Subscriptions))
	rows := map[string][]Row{}
	for _, spec := range loaded.Subscriptions {
		order = append(order, string(spec.ID))
	}
	for _, one := range loaded.Models {
		source := string(one.Subscription)
		rows[source] = append(rows[source], Row{
			Slug:   Slug(one),
			Use:    one.Use,
			Window: one.WindowText(),
			Reason: one.Reason,
		})
	}
	groups := make([]Group, 0, len(order))
	for _, source := range order {
		if len(rows[source]) == 0 {
			continue
		}
		groups = append(groups, Group{Source: source, Rows: rows[source]})
	}
	return Model{Groups: groups}
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m Model) count() int {
	total := 0
	for _, group := range m.Groups {
		total += len(group.Rows)
	}
	return total
}

func (m *Model) Key(key string) {
	switch key {
	case "down", "j":
		m.move(1)
	case "up", "k":
		m.move(-1)
	}
}

func (m *Model) move(by int) {
	if next := m.pick + by; next >= 0 && next < m.count() {
		m.pick = next
	}
}

func (m Model) Picked() (Row, bool) {
	at := 0
	for _, group := range m.Groups {
		for _, row := range group.Rows {
			if at == m.pick {
				return row, true
			}
			at++
		}
	}
	return Row{}, false
}

func (m Model) View() string {
	if m.count() == 0 {
		return strings.Join(pane.Fill([]string{pane.Cell(emptyTitle, m.width, theme.Faint())}, m.height, m.width), "\n")
	}
	lines := []string{pane.Cell(title+gap+m.summary(), m.width, theme.Accent()), pane.Cell("", m.width, theme.Text())}
	slugRoom := m.slugColumn()
	at := 0
	for _, group := range m.Groups {
		lines = append(lines, pane.Cell(group.Source, m.width, theme.Dim()))
		for _, row := range group.Rows {
			lines = append(lines, pane.Cell(m.row(at, row, slugRoom), m.width, m.style(at, row)))
			at++
		}
		lines = append(lines, pane.Cell("", m.width, theme.Text()))
	}
	lines = append(lines, pane.Cell(pickHint, m.width, theme.Faint()))
	return strings.Join(pane.Fill(lines, m.height, m.width), "\n")
}

func (m Model) summary() string {
	count := strconv.Itoa(m.count()) + " models"
	if m.count() == 1 {
		count = "1 model"
	}
	return count
}

func (m Model) style(at int, row Row) lipgloss.Style {
	if row.excluded() {
		return theme.Faint()
	}
	if at == m.pick {
		return theme.Accent()
	}
	return theme.Text()
}

func (m Model) slugColumn() int {
	widest := 0
	for _, group := range m.Groups {
		for _, row := range group.Rows {
			widest = max(widest, widget.Cells(row.Slug))
		}
	}
	return min(widest, m.width/2)
}

func (m Model) row(at int, row Row, slugRoom int) string {
	mark := plainMark
	if at == m.pick {
		mark = pickedMark
	}
	tail := row.Window
	if row.excluded() {
		tail = row.Reason
	}
	tail = widget.Fit(tail, max(m.width-widget.Cells(mark)-slugRoom-widget.Cells(gap), 1))
	return mark + widget.Pad(widget.Fit(row.Slug, slugRoom), slugRoom) + gap + tail
}
