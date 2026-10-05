package models

import (
	"context"
	"errors"
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
	Notice string
	Layer  string
	From   string
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
	Setting             string
}

func (t Target) subAgent() bool { return t.Role == "" && t.Setting == "" }

func (t Target) takes() library.Kind {
	if t.Role == "" {
		return library.KindLLM
	}
	return t.Role.Takes()
}

type Group struct {
	Source  string
	Display string
	Rows    []Row
	Efforts []llm.Effort
	Key     string
	KeySet  bool
}

func (g Group) label() string {
	switch {
	case g.Key == "":
		return g.Source
	case g.KeySet:
		return g.Display + keySetMark
	}
	return g.Display + noKeyMark
}

type Source struct {
	ID      library.Subscription
	Efforts []llm.Effort
}

type Keys struct {
	Set  func(name string) bool
	Save func(ctx context.Context, name, value string) error
}

type keyEntry struct {
	name, provider, slug, typed, refusal string
	checking                             int
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
	Reload
	CheckKey
	CancelCheck
	Set
)

type Intent struct {
	Action  Action
	Slug    string
	Role    library.RoleID
	Agent   string
	Setting string
	Effort  llm.Effort
}

type Model struct {
	Groups      []Group
	classifiers []Group
	sources     []Source
	keys        Keys
	reloading   bool
	entry       keyEntry
	checks      int
	drawn       *drawn
	targets     []Target
	filter      textinput.Model
	tab         tab
	provider    int
	cursor      int
	inUse       []string
	assign      *Target
	effort      llm.Effort
	width       int
	height      int
}

func Build(loaded library.Library, sources []Source, keys Keys, targets []Target) Model {
	rows := map[library.Subscription][]Row{}
	var classifiers, keyed []Group
	for _, one := range loaded.Models {
		row := Row{Slug: one.Slug(), Use: one.Use, Kind: one.Kind, Pays: one.Pays(), Window: one.WindowText(), Reason: one.Reason,
			Notice: one.Notice, Layer: one.Layer, From: one.From}
		switch {
		case one.Kind == library.KindClassifier:
			classifiers = inKeyGroup(classifiers, one, row, keys)
		case one.Kind != library.KindLLM:
			panic("models: unknown model kind " + string(one.Kind))
		case row.Pays == library.PaysKey:
			keyed = inKeyGroup(keyed, one, row, keys)
		default:
			rows[one.Subscription] = append(rows[one.Subscription], row)
		}
	}
	groups := make([]Group, 0, len(sources)+len(keyed))
	for _, source := range sources {
		if len(rows[source.ID]) == 0 {
			continue
		}
		groups = append(groups, Group{Source: string(source.ID), Rows: rows[source.ID], Efforts: source.Efforts})
	}
	groups = append(groups, keyed...)
	filter := textinput.New()
	filter.Prompt, filter.Placeholder = filterPrompt, filterHint
	filter.SetStyles(look.FilterStyles())
	filter.Focus()
	built := Model{Groups: groups, classifiers: classifiers, sources: sources, keys: keys, drawn: &drawn{}, targets: targets, filter: filter, effort: llm.EffortDefault}
	built.settle()
	return built
}

func inKeyGroup(groups []Group, one library.Model, row Row, keys Keys) []Group {
	source, _, _ := strings.Cut(row.Slug, "/")
	at := slices.IndexFunc(groups, func(group Group) bool { return group.Source == source })
	if at < 0 {
		group := Group{Source: source, Efforts: one.Efforts}
		if row.Pays == library.PaysKey {
			group.Key, group.Display = one.Provider.KeyName(), one.Provider.Display()
			group.KeySet = keys.Set(group.Key)
		}
		groups, at = append(groups, group), len(groups)
	}
	groups[at].Rows = append(groups[at].Rows, row)
	return groups
}

func (m Model) Rebuild(loaded library.Library) Model {
	fresh := Build(loaded, m.sources, m.keys, m.targets)
	fresh.SetSize(m.width, m.height)
	fresh.assign = m.assign
	fresh.OpenOn(m.inUse...)
	return fresh
}

func (m *Model) AssignTo(at int) {
	m.assign, m.tab, m.provider = &m.targets[at], tabModels, 0
	m.filter.Reset()
	m.reset()
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m Model) groups() []Group {
	if m.assign != nil && m.assign.takes() == library.KindClassifier {
		return m.classifiers
	}
	return m.Groups
}

func (m Model) groupOf(row Row) Group {
	source, _, _ := strings.Cut(row.Slug, "/")
	for _, group := range m.groups() {
		if group.Source == source {
			return group
		}
	}
	return Group{}
}

func (m Model) missingKey(row Row) string {
	if group := m.groupOf(row); !group.KeySet {
		return group.Key
	}
	return ""
}

func (m Model) providers() []string {
	names := []string{allProviders}
	for _, group := range m.groups() {
		names = append(names, group.label())
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
	for i, group := range m.groups() {
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

func (m *Model) OpenOn(inUse ...string) {
	m.inUse = inUse
	m.home()
}

func (m *Model) home() {
	m.reset()
	if m.tab != tabModels {
		return
	}
	rows := m.visible()
	for _, slug := range m.inUse {
		if at := slices.IndexFunc(rows, func(row Row) bool { return row.Slug == slug }); at >= 0 {
			m.cursor = at
			m.settle()
			return
		}
	}
}

func (m Model) Effort() llm.Effort { return m.effort }

func (m *Model) Key(key string) Intent {
	if m.entry.name != "" {
		return m.entryKey(key)
	}
	switch key {
	case "esc":
		return Intent{Action: Close}
	case "enter":
		return m.choose()
	case "f5":
		if m.reloading {
			return Intent{}
		}
		m.reloading = true
		return Intent{Action: Reload}
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		m.home()
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
		m.home()
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

func (m *Model) entryKey(key string) Intent {
	checking := m.entry.checking != 0
	switch {
	case key == "esc":
		m.entry = keyEntry{}
		if checking {
			return Intent{Action: CancelCheck}
		}
	case checking:
	case key == "enter":
		m.checks++
		m.entry.checking, m.entry.refusal = m.checks, ""
		return Intent{Action: CheckKey}
	case key == "backspace":
		typed := []rune(m.entry.typed)
		m.entry.typed = string(typed[:max(len(typed)-1, 0)])
	default:
		if utf8.RuneCountInString(key) == 1 {
			m.entry.typed, m.entry.refusal = m.entry.typed+key, ""
		}
	}
	return Intent{}
}

type KeyChecked struct {
	check int
	err   error
}

func (m Model) KeyCheck(ctx context.Context) KeyChecked {
	return KeyChecked{check: m.entry.checking, err: m.keys.Save(ctx, m.entry.name, m.entry.typed)}
}

func (m *Model) Checked(result KeyChecked) Intent {
	if result.check == 0 || result.check != m.entry.checking {
		return Intent{}
	}
	if result.err != nil {
		m.entry.checking, m.entry.refusal = 0, result.err.Error()
		if refused := (*library.KeyRefused)(nil); errors.As(result.err, &refused) {
			m.entry.refusal = refused.Brief()
		}
		return Intent{}
	}
	for _, groups := range [][]Group{m.classifiers, m.Groups} {
		for i := range groups {
			groups[i].KeySet = groups[i].KeySet || groups[i].Key == m.entry.name
		}
	}
	m.entry = keyEntry{}
	return m.choose()
}

func (m *Model) Paste(text string) {
	text = strings.TrimSpace(text)
	switch {
	case m.entry.name == "":
		m.setFilter(m.filter.Value() + text)
	case m.entry.checking == 0:
		m.entry.typed, m.entry.refusal = m.entry.typed+text, ""
	}
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
	return m.groupOf(row).Efforts
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
	missing := m.missingKey(row)
	switch {
	case !picked:
		return Intent{}
	case row.excluded():
		return Intent{Action: Login, Slug: row.Slug}
	case missing != "":
		m.entry = keyEntry{name: missing, provider: m.groupOf(row).Display, slug: row.Slug}
		return Intent{}
	case m.assign == nil:
		return Intent{Action: Pick, Slug: row.Slug, Effort: m.effort}
	}
	target := m.assign
	m.assign, target.Assigned = nil, row.Slug
	switch {
	case target.Setting != "":
		return Intent{Action: Set, Setting: target.Setting, Agent: target.Name, Slug: row.Slug}
	case target.subAgent():
		return Intent{Action: Assign, Agent: target.Name, Slug: row.Slug}
	}
	return Intent{Action: Bind, Role: target.Role, Slug: row.Slug}
}

func (m *Model) Click(x, y int) Intent {
	left, top := origin(m.width, m.height)
	lines := strings.Split(m.cached(m.width, m.height), "\n")
	row, at := y-top, x-left
	if m.entry.name != "" || row < 0 || row >= len(lines) || at < 0 {
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
	m.home()
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
