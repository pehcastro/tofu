package models

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/pane"
	"tofu/interface/tui/theme"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	"tofu/internal/widget"
)

const (
	minimumWidth      = 20
	beforeTheFirstRow = -1
	gap               = "  "
	pickedMark        = "› "
	plainMark         = "  "
	title             = "models"
	effortHead        = "effort "
	pickHint          = "↑↓ choose   ←→ effort   enter picks"
	emptyTitle        = "the library has no model to pick"
)

type Row struct {
	Slug   string
	Use    library.Use
	Window string
	Reason string
}

func (r Row) excluded() bool { return r.Use == library.UseExcluded }

type Group struct {
	Source  string
	Rows    []Row
	Efforts []llm.Effort
}

type Source struct {
	ID      library.Subscription
	Efforts []llm.Effort
}

type Model struct {
	Groups []Group
	pick   int
	effort llm.Effort
	width  int
	height int
}

func Build(loaded library.Library, sources []Source) Model {
	rows := map[library.Subscription][]Row{}
	for _, one := range loaded.Models {
		rows[one.Subscription] = append(rows[one.Subscription], Row{
			Slug:   one.Slug(),
			Use:    one.Use,
			Window: one.WindowText(),
			Reason: one.Reason,
		})
	}
	groups := make([]Group, 0, len(sources))
	for _, source := range sources {
		if len(rows[source.ID]) == 0 {
			continue
		}
		groups = append(groups, Group{Source: string(source.ID), Rows: rows[source.ID], Efforts: source.Efforts})
	}
	built := Model{Groups: groups, pick: beforeTheFirstRow, effort: llm.EffortDefault}
	built.move(1)
	return built
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
	case "right", "l":
		m.step(1)
	case "left", "h":
		m.step(-1)
	}
}

func (m *Model) step(by int) {
	offered := m.offered()
	at := slices.Index(offered, m.effort)
	if at < 0 || at+by < 0 || at+by >= len(offered) {
		return
	}
	m.effort = offered[at+by]
}

func (m Model) offered() []llm.Effort {
	_, group, _ := m.rowAt(m.pick)
	return group.Efforts
}

func (m Model) Effort() llm.Effort { return m.effort }

func (m *Model) settle() {
	offered := m.offered()
	switch {
	case slices.Contains(offered, m.effort):
	case slices.Contains(offered, llm.EffortDefault):
		m.effort = llm.EffortDefault
	case len(offered) > 0:
		m.effort = offered[0]
	default:
		m.effort = ""
	}
}

func (m *Model) move(by int) {
	for next := m.pick + by; next >= 0 && next < m.count(); next += by {
		if row, _, _ := m.rowAt(next); !row.excluded() {
			m.pick = next
			m.settle()
			return
		}
	}
}

func (m Model) rowAt(at int) (Row, Group, bool) {
	if at < 0 {
		return Row{}, Group{}, false
	}
	for _, group := range m.Groups {
		if at < len(group.Rows) {
			return group.Rows[at], group, true
		}
		at -= len(group.Rows)
	}
	return Row{}, Group{}, false
}

func (m Model) Picked() (Row, bool) {
	row, _, picked := m.rowAt(m.pick)
	return row, picked
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
	if m.effort == "" {
		return count
	}
	return count + gap + effortHead + string(m.effort)
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
		widest = widget.Column(group.Rows, func(row Row) string { return row.Slug }, widest)
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
