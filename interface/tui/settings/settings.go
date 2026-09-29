package settings

import (
	"slices"
	"strconv"
	"strings"

	isettings "tofu/internal/settings"
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
	RowSubAgent
	RowModel
)

type Number struct {
	Least, Most    int
	DefaultMeaning string
}

type Row struct {
	Key, Category, Label, Description, Value, Source string
	Origin, Path                                     string
	Kind                                             Kind
	Choices                                          []string
	Number                                           Number
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

type numberState struct {
	open           bool
	typed, refusal string
}

type inspectorScroll struct {
	key string
	top int
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
	dialog         choiceState
	search         searchState
	number         numberState
	scrolled       inspectorScroll
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

func (m *Model) Typing() bool { return m.search.open || m.number.open }

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
	if m.number.open {
		return m.numberInput(key)
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
	case "pgup":
		m.scrollInspector(-m.height / 2)
	case "pgdown":
		m.scrollInspector(m.height / 2)
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

func (m *Model) Wheel(x, delta int) Intent {
	step := 1
	if delta < 0 {
		step = -1
	}
	geo := m.layout()
	switch {
	case !m.dialog.open && !m.search.open && !m.number.open && geo.inspector > 0 && x >= m.width-geo.inspector:
		m.scrollInspector(step)
	case m.dialog.open:
		row, _ := m.selected()
		m.dialog.cursor = min(max(m.dialog.cursor+step, 0), len(row.Choices)-1)
		return Intent{Action: ActionPreview, Key: row.Key, Value: row.Choices[m.dialog.cursor]}
	case m.search.open:
		m.search.cursor = min(max(m.search.cursor+step, 0), max(0, len(m.matches())-1))
	case m.number.open:
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
	case RowRole, RowSubAgent, RowModel:
		return Intent{Action: ActionOpenRole, Key: row.Key}
	case RowValue:
	default:
		panic("settings: unknown row action")
	}
	switch {
	case row.Kind == Bool:
		return Intent{Action: ActionToggle, Key: row.Key}
	case row.Kind == Int:
		m.number = numberState{open: true}
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

func (m *Model) numberInput(key string) Intent {
	row, _ := m.selected()
	switch key {
	case "esc":
		m.number = numberState{}
	case "backspace":
		m.number.typed = m.number.typed[:max(0, len(m.number.typed)-1)]
	case "enter":
		value, err := strconv.Atoi(m.number.typed)
		if err != nil || value < row.Number.Least || value > row.Number.Most {
			m.number.refusal = isettings.Refusal(m.number.typed, row.Number.Least, row.Number.Most)
			return Intent{}
		}
		m.number = numberState{}
		return Intent{Action: ActionCommit, Key: row.Key, Value: strconv.Itoa(value)}
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && len(m.number.typed) < numberMaxDigits {
			m.number.typed += key
			m.number.refusal = ""
		}
	}
	return Intent{}
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
	if found := m.matches(); len(found) > 0 {
		m.Jump(m.Rows[found[m.search.cursor]].Key)
	}
}

func (m *Model) Jump(key string) bool {
	at := slices.IndexFunc(m.Rows, func(row Row) bool { return row.Key == key })
	if at < 0 {
		return false
	}
	m.search, m.Query = searchState{}, ""
	m.category = slices.Index(m.categories(), m.Rows[at].Category)
	m.cursor = slices.Index(m.inCategory(), at)
	return true
}

func (m *Model) CloseDialog() Intent {
	m.number = numberState{}
	if !m.dialog.open {
		return Intent{}
	}
	return m.dialogKey("esc")
}
