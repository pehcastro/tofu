package models

import (
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
)

const (
	minimumWidth = 20
	tabsTop      = 4
	providerTop  = 7
	catalogTop   = 6
	rolesTop     = 4
	linesPerRow  = 2
	allProviders = "all"
	modelsTab    = "Models"
	rolesTab     = "Roles"
	filterPrompt = "> "
	filterHint   = "Type to filter"
	disabledSlug = "none"
	inheritSlug  = "inherit"
)

type Row struct {
	Slug   string
	Label  string
	Use    library.Use
	Kind   library.Kind
	Pays   library.Pays
	Window string
	Reason string
}

func (r Row) excluded() bool { return r.Use == library.UseExcluded }

func (r Row) name() (source, model string) {
	if r.Label != "" {
		return r.Reason, r.Label
	}
	source, model, _ = strings.Cut(r.Slug, "/")
	return source, model
}

type Target struct {
	Name, Job, Assigned string
	Role                library.RoleID
}

func (t Target) subAgent() bool { return t.Role == "" }

type Group struct {
	Source  string
	Rows    []Row
	Efforts []llm.Effort
}

type Source struct {
	ID      library.Subscription
	Efforts []llm.Effort
}

type tab int

const (
	tabModels tab = iota
	tabRoles
	tabCount
)

type Action int

const (
	None Action = iota
	Pick
	Bind
	Assign
	Login
	Close
)

type Intent struct {
	Action Action
	Slug   string
	Role   library.RoleID
	Agent  string
	Effort llm.Effort
}

type Model struct {
	Groups   []Group
	drawn    *drawn
	targets  []Target
	filter   textinput.Model
	tab      tab
	provider int
	cursor   int
	assign   *Target
	effort   llm.Effort
	width    int
	height   int
}

func Build(loaded library.Library, sources []Source, targets []Target) Model {
	rows := map[library.Subscription][]Row{}
	for _, one := range loaded.Models {
		if one.Kind != library.KindLLM {
			continue
		}
		rows[one.Subscription] = append(rows[one.Subscription], Row{
			Slug:   one.Slug(),
			Use:    one.Use,
			Kind:   one.Kind,
			Pays:   one.Pays(),
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
	filter := textinput.New()
	filter.Prompt, filter.Placeholder = filterPrompt, filterHint
	filter.SetStyles(look.FilterStyles())
	filter.Focus()
	built := Model{Groups: groups, drawn: &drawn{}, targets: targets, filter: filter, effort: llm.EffortDefault}
	built.settle()
	return built
}

func (m *Model) AssignTo(at int) {
	m.assign, m.tab = &m.targets[at], tabModels
	m.filter.Reset()
	m.reset()
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m Model) providers() []string {
	names := []string{allProviders}
	for _, group := range m.Groups {
		names = append(names, group.Source)
	}
	return names
}

func (m Model) visible() []Row {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	var rows []Row
	if m.assign != nil && m.assign.subAgent() && m.provider == 0 {
		for _, row := range []Row{
			{Slug: disabledSlug, Label: "none (disabled)", Reason: "the orchestrator is never offered it"},
			{Slug: inheritSlug, Label: "inherit", Reason: "runs on the orchestrator's model"},
		} {
			if strings.Contains(row.Label, query) {
				rows = append(rows, row)
			}
		}
	}
	for i, group := range m.Groups {
		if m.provider != 0 && m.provider != i+1 {
			continue
		}
		for _, row := range group.Rows {
			if strings.Contains(strings.ToLower(row.Slug+" "+row.Window), query) {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func (m Model) Picked() (Row, bool) {
	rows := m.visible()
	if m.cursor >= len(rows) {
		return Row{}, false
	}
	return rows[m.cursor], true
}

func (m Model) Effort() llm.Effort { return m.effort }

func (m *Model) Key(key string) Intent {
	switch key {
	case "esc":
		return Intent{Action: Close}
	case "enter":
		return m.choose()
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		m.reset()
	case "left", "right":
		count := len(m.providers())
		switch {
		case m.tab == tabRoles:
			m.tab = tabModels
		case key == "left":
			m.provider = (m.provider + count - 1) % count
		default:
			m.provider = (m.provider + 1) % count
		}
		m.reset()
	case "shift+left":
		m.step(-1)
	case "shift+right":
		m.step(1)
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "backspace":
		value := []rune(m.filter.Value())
		m.setFilter(string(value[:max(len(value)-1, 0)]))
	case "space":
		m.setFilter(m.filter.Value() + " ")
	default:
		if utf8.RuneCountInString(key) == 1 {
			m.setFilter(m.filter.Value() + key)
		}
	}
	return Intent{}
}

func (m *Model) setFilter(value string) {
	if m.tab != tabModels {
		return
	}
	m.filter.SetValue(value)
	m.filter.CursorEnd()
	m.reset()
}

func (m *Model) reset() {
	m.cursor = 0
	m.settle()
}

func (m Model) count() int {
	if m.tab == tabRoles {
		return len(m.targets)
	}
	return len(m.visible())
}

func (m *Model) move(by int) {
	count := m.count()
	if count == 0 {
		return
	}
	m.cursor = (m.cursor + by + count) % count
	m.settle()
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
	row, picked := m.Picked()
	if !picked {
		return nil
	}
	source, _, _ := strings.Cut(row.Slug, "/")
	for _, group := range m.Groups {
		if group.Source == source {
			return group.Efforts
		}
	}
	return nil
}

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

func (m *Model) choose() Intent {
	if m.tab == tabRoles {
		if m.cursor < len(m.targets) {
			m.AssignTo(m.cursor)
		}
		return Intent{}
	}
	row, picked := m.Picked()
	switch {
	case !picked:
		return Intent{}
	case row.excluded():
		return Intent{Action: Login, Slug: row.Slug}
	case m.assign == nil:
		return Intent{Action: Pick, Slug: row.Slug, Effort: m.effort}
	}
	target := m.assign
	m.assign, target.Assigned = nil, row.Slug
	if target.subAgent() {
		return Intent{Action: Assign, Agent: target.Name, Slug: row.Slug}
	}
	return Intent{Action: Bind, Role: target.Role, Slug: row.Slug}
}

func (m *Model) Click(x, y int) Intent {
	left, top := origin(m.width, m.height)
	lines := strings.Split(m.cached(m.width, m.height), "\n")
	row, at := y-top, x-left
	if row < 0 || row >= len(lines) || at < 0 {
		return Intent{}
	}
	modalWidth, _ := modelDialogSize(m.width, m.height)
	side, list, _ := modelPaneWidths(modalWidth)
	switch {
	case at < side:
		m.clickSide(ansi.Strip(ansi.Cut(lines[row], 0, side)), at, row)
	case at < side+list:
		return m.clickList(ansi.Strip(ansi.Cut(lines[row], side, side+list)), at-side, row)
	}
	return Intent{}
}

func (m *Model) clickSide(line string, x, row int) {
	providers := m.providers()
	switch at := row - providerTop; {
	case row == tabsTop && pointer.TextHit(line, modelsTab, x):
		m.tab = tabModels
	case row == tabsTop && pointer.TextHit(line, rolesTab, x):
		m.tab = tabRoles
	case at >= 0 && at < len(providers) && pointer.TextHit(line, providers[at], x):
		m.provider, m.tab = at, tabModels
	default:
		return
	}
	m.reset()
}

func (m *Model) clickList(line string, x, row int) Intent {
	top, labels := catalogTop, []string{}
	start, end := m.page()
	if m.tab == tabRoles {
		top = rolesTop
		for _, target := range m.targets[start:end] {
			labels = append(labels, target.Name)
		}
	} else {
		for _, one := range m.visible()[start:end] {
			_, model := one.name()
			labels = append(labels, model)
		}
	}
	at := (row - top) / linesPerRow
	if row < top || (row-top)%linesPerRow != 0 || at >= len(labels) || !pointer.TextHit(line, labels[at], x) {
		return Intent{}
	}
	m.cursor = start + at
	m.settle()
	return m.choose()
}
