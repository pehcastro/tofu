package settings

import (
	"slices"
	"strings"
)

type Kind int

const (
	Bool Kind = iota
	Int
	Text
)

type RowAction int

const (
	RowValue RowAction = iota
	RowKeybindings
	RowHostIntegration
	RowRole
)

type Row struct {
	Key, Category, Label, Description, Value, Source string
	Kind                                             Kind
	Choices                                          []string
	Changed, RestartRequired, RestartPending         bool
	Action                                           RowAction
}

type Action int

const (
	ActionNone Action = iota
	ActionPreview
	ActionCommit
	ActionRevert
	ActionToggle
	ActionIncrement
	ActionDecrement
	ActionCycleScope
	ActionOpenKeybindings
	ActionOpenHost
	ActionOpenRole
	ActionClose
)

type Intent struct {
	Action Action
	Key    string
	Value  string
}

type choiceState struct {
	open     bool
	key      string
	cursor   int
	original string
}

type searchState struct {
	open   bool
	cursor int
}

type Model struct {
	Providers      []Provider
	ChatShowsTools bool
	Rows           []Row
	Scopes         []string
	Scope          int
	Query          string
	category       int
	cursor         int
	width          int
	height         int
	searchKey      string
	density        string
	branch         string
	previewTheme   func(string) string
	dialog         choiceState
	search         searchState
	cache          viewCache
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), max(height, minimumHeight)
}

func (m *Model) SetRows(rows []Row) {
	m.Rows = rows
	m.category = min(m.category, max(0, len(m.categories())-1))
	m.cursor = min(m.cursor, max(0, len(m.inCategory())-1))
}

func (m *Model) SetSearchKey(key string) { m.searchKey = key }

func (m *Model) SetDensity(density string) { m.density = density }

func (m *Model) SetBranch(branch string) { m.branch = branch }

func (m *Model) SetPreviewTheme(recolour func(string) string) {
	m.previewTheme = recolour
	m.cache = viewCache{}
}

func (m *Model) Searching() bool { return m.search.open }

func (m *Model) categories() []string {
	var names []string
	for _, row := range m.Rows {
		if !slices.Contains(names, row.Category) {
			names = append(names, row.Category)
		}
	}
	return names
}

func (m *Model) inCategory() []int {
	names := m.categories()
	if len(names) == 0 {
		return nil
	}
	var indices []int
	for i, row := range m.Rows {
		if row.Category == names[m.category] {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *Model) selected() (Row, bool) {
	indices := m.inCategory()
	if len(indices) == 0 {
		return Row{}, false
	}
	return m.Rows[indices[m.cursor]], true
}

func (m *Model) Key(key string) Intent {
	if m.dialog.open {
		return m.dialogKey(key)
	}
	if m.search.open {
		return m.searchInput(key)
	}
	switch key {
	case m.searchKey:
		m.search = searchState{open: true}
		m.Query = ""
	case "esc":
		return Intent{Action: ActionClose}
	case "left":
		m.moveCategory(-1)
	case "right":
		m.moveCategory(1)
	case "up":
		m.moveRow(-1)
	case "down":
		m.moveRow(1)
	case "tab":
		return Intent{Action: ActionCycleScope}
	case "+", "=":
		return m.step(ActionIncrement)
	case "-":
		return m.step(ActionDecrement)
	case "enter", "space":
		return m.activate()
	}
	return Intent{}
}

func (m *Model) Wheel(delta int) Intent {
	step := 1
	if delta < 0 {
		step = -1
	}
	switch {
	case m.dialog.open:
		row, _ := m.selected()
		m.dialog.cursor = min(max(m.dialog.cursor+step, 0), len(row.Choices)-1)
		return Intent{Action: ActionPreview, Key: row.Key, Value: row.Choices[m.dialog.cursor]}
	case m.search.open:
		m.search.cursor = min(max(m.search.cursor+step, 0), max(0, len(m.matches())-1))
	default:
		m.moveRow(step)
	}
	return Intent{}
}

func (m *Model) moveRow(by int) {
	m.cursor = min(max(m.cursor+by, 0), max(0, len(m.inCategory())-1))
}

func (m *Model) moveCategory(by int) {
	count := len(m.categories())
	if count == 0 {
		return
	}
	m.category = (m.category + by + count) % count
	m.cursor = 0
}

func (m *Model) step(action Action) Intent {
	row, ok := m.selected()
	if !ok || row.Kind != Int {
		return Intent{}
	}
	return Intent{Action: action, Key: row.Key}
}

func (m *Model) activate() Intent {
	row, ok := m.selected()
	if !ok {
		return Intent{}
	}
	switch row.Action {
	case RowKeybindings:
		return Intent{Action: ActionOpenKeybindings, Key: row.Key}
	case RowHostIntegration:
		return Intent{Action: ActionOpenHost, Key: row.Key}
	case RowRole:
		return Intent{Action: ActionOpenRole, Key: row.Key}
	case RowValue:
	default:
		panic("settings: unknown row action")
	}
	switch {
	case row.Kind == Bool:
		return Intent{Action: ActionToggle, Key: row.Key}
	case row.Kind == Int:
		return Intent{Action: ActionIncrement, Key: row.Key}
	case len(row.Choices) > 0:
		m.dialog = choiceState{open: true, key: row.Key, cursor: max(0, slices.Index(row.Choices, row.Value)), original: row.Value}
	}
	return Intent{}
}

func (m *Model) dialogKey(key string) Intent {
	row, _ := m.selected()
	count := len(row.Choices)
	switch key {
	case "up":
		m.dialog.cursor = (m.dialog.cursor + count - 1) % count
	case "down":
		m.dialog.cursor = (m.dialog.cursor + 1) % count
	case "enter", "space":
		return m.commit(row)
	case "esc":
		original := m.dialog.original
		m.dialog = choiceState{}
		return Intent{Action: ActionRevert, Key: row.Key, Value: original}
	default:
		return Intent{}
	}
	return Intent{Action: ActionPreview, Key: row.Key, Value: row.Choices[m.dialog.cursor]}
}

func (m *Model) commit(row Row) Intent {
	value := row.Choices[m.dialog.cursor]
	m.dialog = choiceState{}
	return Intent{Action: ActionCommit, Key: row.Key, Value: value}
}

func (m *Model) searchInput(key string) Intent {
	count := len(m.matches())
	switch key {
	case "esc":
		m.search = searchState{}
		m.Query = ""
	case "up":
		m.search.cursor = (m.search.cursor + count - 1) % max(1, count)
	case "down":
		m.search.cursor = (m.search.cursor + 1) % max(1, count)
	case "enter":
		m.jump()
	case "backspace":
		runes := []rune(m.Query)
		m.Query = string(runes[:max(0, len(runes)-1)])
		m.search.cursor = 0
	default:
		if key == "space" {
			key = " "
		}
		if runes := []rune(key); len(runes) == 1 && runes[0] >= ' ' {
			m.Query += key
			m.search.cursor = 0
		}
	}
	return Intent{}
}

func (m *Model) matches() []int {
	query := strings.ToLower(strings.TrimSpace(m.Query))
	var found []int
	for i, row := range m.Rows {
		if strings.Contains(strings.ToLower(row.Category+" "+row.Label+" "+row.Description), query) {
			found = append(found, i)
		}
	}
	return found
}

func (m *Model) jump() {
	found := m.matches()
	if len(found) == 0 {
		return
	}
	target := m.Rows[found[m.search.cursor]]
	m.search = searchState{}
	m.Query = ""
	m.category = slices.Index(m.categories(), target.Category)
	m.cursor = slices.IndexFunc(m.inCategory(), func(i int) bool { return m.Rows[i].Key == target.Key })
}
