package edits

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
)

const (
	editWindow   = 500
	minimumWidth = 20
	sidebarMax   = 36
	sidebarShare = 3
	narrowWidth  = 86
	padding      = 2
	cardInset    = 6
	nameFloor    = 4
	indexChrome  = 4
	wheelRows    = 3
	title        = "File edits"
	editsLabel   = "Edits"
	allChanges   = "All changes"
	agentMark    = "&"
	indexHint    = "Select an edit reference to read every changed line."
	noEdits      = "No file edits for this author"
	emptyTitle   = "no file has changed in this session"
	emptyBody    = "when the turn edits or writes a file the change appears here with its diff, and the session keeps one row saying which file changed and by how much."
)

type sideKey struct {
	width, height, count int
	author, selected     string
	first, last, authors string
	focused              bool
	from                 int
}

type listKey struct {
	width, count              int
	author, first, last, root string
	plain                     bool
}

type trimKey struct {
	id           string
	contextLines int
}

type caches struct {
	rows     diffRowIndex
	side     sideKey
	sideView string
	list     listKey
	listView string
	main     look.PaneCache
	trim     trimKey
	trimmed  Edit
	lexers   map[string]chroma.Lexer
}

type Preferences struct {
	ContextLines int
	PlainPaths   bool
	NoAuthors    bool
}

type Model struct {
	SubAgents []subagent.Row
	Busy      bool
	Root      string
	prefs     Preferences
	edits     []Edit
	author    string
	selected  string
	reading   bool
	wide      bool
	focusMain bool
	scroll    int
	sideFrom  int
	width     int
	height    int
	cache     *caches
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
	if m.cache == nil {
		m.cache = &caches{}
	}
}

func (m *Model) SetPreferences(prefs Preferences) {
	m.prefs = prefs
	if prefs.NoAuthors {
		m.author = ""
	}
}

func (m Model) shown(edit Edit) Edit {
	key := trimKey{id: edit.ID, contextLines: m.prefs.ContextLines}
	if m.cache != nil && m.cache.trim == key {
		return m.cache.trimmed
	}
	trimmed := edit.within(m.prefs.ContextLines)
	if m.cache != nil {
		m.cache.trim, m.cache.trimmed = key, trimmed
	}
	return trimmed
}

func (m *Model) Add(edit Edit) {
	m.lexer(edit.Path)
	m.edits = append(m.edits, edit)
	if len(m.edits) > editWindow {
		m.edits = m.edits[len(m.edits)-editWindow:]
	}
}

func (m *Model) Key(key string) bool {
	switch key {
	case "esc":
		if !m.reading {
			return false
		}
		m.reading, m.scroll = false, 0
	case "left":
		m.focusMain = false
	case "right":
		m.focusMain = true
	case "up", "k":
		m.move(1, -1)
	case "down", "j":
		m.move(-1, 1)
	case "pgup":
		m.scrollFocused(m.pane().visible, -m.sideRows())
	case "pgdown":
		m.scrollFocused(-m.pane().visible, m.sideRows())
	case "home":
		m.scrollFocused(m.pane().total, -len(m.edits))
	case "end":
		m.scrollFocused(-m.scroll, len(m.edits))
	case "enter":
		switch {
		case !m.focusMain:
			m.focusMain = true
		case !m.reading:
			if edit, _, ok := chosen(m.visible(), m.selected); ok {
				m.open(edit.ID)
			}
		default:
			m.wide = !m.wide
		}
	case "n":
		m.step(1)
	case "p":
		m.step(-1)
	case "f":
		m.wide, m.focusMain = !m.wide, true
	}
	return true
}

func (m *Model) Wheel(up bool) {
	if up {
		m.scrollFocused(wheelRows, -wheelRows)
		return
	}
	m.scrollFocused(-wheelRows, wheelRows)
}

func (m *Model) scrollFocused(main, side int) {
	if m.focusMain || m.Split() == 0 {
		m.scrollBy(main)
		return
	}
	m.sideFrom = min(max(0, m.sideFrom+side), max(0, len(files(m.visible()))-m.sideRows()))
}

func (m *Model) SetScroll(behindNewest int) { m.scrollBy(behindNewest - m.scroll) }

func (m *Model) Open(id string) bool {
	at := slices.IndexFunc(m.edits, func(edit Edit) bool { return trace.Short(edit.ID) == trace.Short(id) })
	if at < 0 {
		return false
	}
	if m.edits[at].Agent != m.author {
		m.author = ""
	}
	m.open(m.edits[at].ID)
	return true
}

func (m *Model) Click(x, y int) {
	view := m.View()
	side := m.Split()
	m.focusMain = x >= side
	if x < side {
		lines := strings.Split(view, "\n")
		if y < 0 || y >= len(lines) {
			return
		}
		line := ansi.Strip(ansi.Cut(lines[y], 0, side))
		for _, name := range append([]string{""}, m.authors()...) {
			if pointer.TextHit(line, label(name)+" ", x) {
				m.pickAuthor(name)
				m.focusMain = false
				return
			}
		}
	}
	if kind, id := pointer.SplitReference(pointer.ReferenceAt(view, x, y)); kind == editKind {
		m.Open(strings.TrimPrefix(id, "#"))
	}
}

func (m *Model) move(scroll, author int) {
	if m.focusMain {
		m.scrollBy(scroll)
		return
	}
	names := append([]string{""}, m.authors()...)
	at := max(0, slices.Index(names, m.author))
	m.pickAuthor(names[(at+author+len(names))%len(names)])
}

func (m *Model) pickAuthor(name string) {
	m.author, m.reading, m.scroll, m.sideFrom = name, false, 0, 0
}

func (m *Model) open(id string) {
	m.selected, m.reading, m.scroll, m.focusMain = id, true, 0, true
}

func (m *Model) step(by int) {
	visible := m.visible()
	if len(visible) == 0 {
		return
	}
	at := slices.IndexFunc(visible, func(edit Edit) bool { return edit.ID == m.selected })
	if at < 0 && by < 0 {
		at = 0
	}
	m.open(visible[(at+by+len(visible))%len(visible)].ID)
}

func (m *Model) scrollBy(rows int) {
	shown := m.pane()
	m.focusMain = true
	m.scroll = min(max(0, m.scroll+rows), max(0, shown.total-shown.visible))
}

func (m Model) authors() []string {
	names := []string{}
	if m.prefs.NoAuthors {
		return names
	}
	edited := func(name string) bool {
		return slices.ContainsFunc(m.edits, func(edit Edit) bool { return edit.Agent == name })
	}
	if edited(Self) {
		names = append(names, Self)
	}
	for _, subAgent := range m.SubAgents {
		if edited(subAgent.Name) && !slices.Contains(names, subAgent.Name) {
			names = append(names, subAgent.Name)
		}
	}
	for _, edit := range m.edits {
		if !slices.Contains(names, edit.Agent) {
			names = append(names, edit.Agent)
		}
	}
	return names
}

func label(name string) string {
	if name == "" {
		return allChanges
	}
	return agentMark + name
}

func (m Model) visible() []Edit {
	if m.author == "" {
		return m.edits
	}
	var shown []Edit
	for _, edit := range m.edits {
		if edit.Agent == m.author {
			shown = append(shown, edit)
		}
	}
	return shown
}

func chosen(visible []Edit, id string) (Edit, int, bool) {
	if len(visible) == 0 {
		return Edit{}, 0, false
	}
	if at := slices.IndexFunc(visible, func(edit Edit) bool { return edit.ID == id }); at >= 0 {
		return visible[at], at, true
	}
	return visible[len(visible)-1], len(visible) - 1, true
}

func (m Model) Split() int {
	if m.width < narrowWidth || m.wide || len(m.edits) == 0 {
		return 0
	}
	return min(sidebarMax, m.width/sidebarShare)
}

func (m Model) mainWidth() int {
	if side := m.Split(); side > 0 {
		return m.width - side - 2*padding
	}
	return m.width
}

func (m Model) pane() window {
	visible := m.visible()
	edit, _, ok := chosen(visible, m.selected)
	switch {
	case m.reading && ok:
		_, shown := m.diffWindow(m.shown(edit), max(diffMinWidth, m.mainWidth()), max(diffMinHeight, m.height-1))
		return shown
	case !m.reading:
		return scrolled(strings.Count(m.indexList(m.mainWidth(), visible), "\n")+1, max(1, m.height-1-indexChrome), m.scroll)
	}
	return window{}
}

func (m Model) Track() pointer.Track {
	if len(m.edits) == 0 {
		return pointer.Track{}
	}
	shown := m.pane()
	return pointer.Track{Total: shown.total, Visible: shown.visible, FromTop: shown.from}
}

func (m Model) View() string {
	if len(m.edits) == 0 {
		return look.Surface(m.width, m.height, "", padding, "\n"+look.Title(emptyTitle)+"\n\n"+look.Muted(emptyBody))
	}
	side, width := m.Split(), m.mainWidth()
	visible := m.visible()
	edit, index, ok := chosen(visible, m.selected)
	main := look.Muted(noEdits)
	switch {
	case !m.reading:
		main = m.indexView(width, m.height-1, visible)
	case ok:
		main = m.completeFileDiff(width, m.height-1, m.shown(edit), index, len(visible))
	}
	if side == 0 {
		return look.FixedBlock(m.width, m.height, main)
	}
	selected := ""
	if ok && m.reading {
		selected = edit.ID
	}
	pane := &look.PaneCache{}
	if m.cache != nil {
		pane = &m.cache.main
	}
	return look.JoinFixedPanes(m.sidebar(side, visible, selected), pane.Surface(m.width-side, m.height, "", padding, "\n"+main))
}

type fileRow struct {
	latest Edit
	edits  int
}

func files(visible []Edit) []fileRow {
	var rows []fileRow
	at := map[string]int{}
	for index := len(visible) - 1; index >= 0; index-- {
		edit := visible[index]
		if row, seen := at[edit.Path]; seen {
			rows[row].edits++
			continue
		}
		at[edit.Path] = len(rows)
		rows = append(rows, fileRow{latest: edit, edits: 1})
	}
	return rows
}

func (m Model) sideRows() int {
	side := m.Split()
	if side == 0 {
		return 0
	}
	return max(1, m.height-strings.Count(m.sideHeader(side-2*padding, m.authors()), "\n"))
}

func (m Model) sidebar(width int, visible []Edit, selected string) string {
	authors := m.authors()
	key := sideKey{width: width, height: m.height, count: len(m.edits), author: m.author, selected: selected, first: m.edits[0].ID, last: m.edits[len(m.edits)-1].ID, authors: strings.Join(authors, "\n"), focused: !m.focusMain, from: m.sideFrom}
	if m.cache != nil && m.cache.sideView != "" && m.cache.side == key {
		return m.cache.sideView
	}
	inner := width - 2*padding
	selectedPath := ""
	if at := slices.IndexFunc(visible, func(edit Edit) bool { return edit.ID == selected }); at >= 0 {
		selectedPath = visible[at].Path
	}
	var content strings.Builder
	content.WriteString(m.sideHeader(inner, authors))
	rows := files(visible)
	from := min(m.sideFrom, max(0, len(rows)-1))
	for _, row := range rows[from:min(len(rows), from+m.sideRows())] {
		_, sigil, colour := row.latest.op.mark()
		line := look.Style(colour).Render(sigil) + " " + look.TypedID(editKind, trace.Short(row.latest.ID)) + " "
		count := ""
		if row.edits > 1 {
			count = " x" + strconv.Itoa(row.edits)
		}
		name := filepath.Base(row.latest.Path)
		if over := ansi.StringWidth(name) - max(nameFloor, inner-lipgloss.Width(line)-len(count)); over > 0 {
			name = ansi.TruncateLeft(name, over+1, "…")
		}
		background := look.Panel
		if row.latest.Path == selectedPath {
			background = look.PanelLight
		}
		content.WriteString(lipgloss.NewStyle().Width(inner).Background(lipgloss.Color(string(background))).Render(look.KeepSurfaceBackground(line+look.Faint(name)+look.Muted(count), background)) + "\n")
	}
	view := look.Surface(width, m.height, look.Panel, padding, content.String())
	if m.cache != nil {
		m.cache.side, m.cache.sideView = key, view
	}
	return view
}

func (m Model) sideHeader(inner int, authors []string) string {
	paths := map[string]bool{}
	kinds := map[Op]int{}
	added, removed := map[string]int{}, map[string]int{}
	for _, edit := range m.edits {
		paths[edit.Path] = true
		kinds[edit.op]++
		added[""], removed[""] = added[""]+edit.added, removed[""]+edit.removed
		added[edit.Agent], removed[edit.Agent] = added[edit.Agent]+edit.added, removed[edit.Agent]+edit.removed
	}
	var content strings.Builder
	content.WriteString("\n" + look.PaneTitle(title, !m.focusMain) + "\n")
	content.WriteString(counts(inner, fmt.Sprint(len(paths), " files"), fmt.Sprint(len(m.edits), " changes")) + "\n")
	content.WriteString(counts(inner, fmt.Sprint(kinds[OpModified], " mod"), fmt.Sprint(kinds[OpAdded], " new"), fmt.Sprint(kinds[OpDeleted], " del")) + "\n")
	content.WriteString(deltas(added[""], removed[""]) + look.Muted(" lines") + "\n\n")
	if !m.prefs.NoAuthors {
		content.WriteString(look.SectionLabel("Author") + "\n")
		content.WriteString(look.SidebarItem(inner, m.author == "", allChanges, strconv.Itoa(len(m.edits))) + "\n")
		for _, name := range authors {
			content.WriteString(look.SidebarDeltaItem(inner, m.author == name, label(name), added[name], removed[name]) + "\n")
		}
		content.WriteString("\n")
	}
	return content.String() + look.SectionLabel(editsLabel) + "\n"
}

func counts(width int, parts ...string) string {
	line := strings.Join(parts, "  ·  ")
	if ansi.StringWidth(line) > width {
		line = strings.Join(parts, " · ")
	}
	return look.Muted(ansi.Truncate(line, width, "…"))
}

func (m Model) indexView(width, height int, visible []Edit) string {
	header := look.Sides(look.Title(label(m.author)), look.Faint(fmt.Sprintf("%d edits · oldest → newest", len(visible))), width-1)
	view, _, _ := look.Window(m.indexList(width, visible), width, max(1, height-indexChrome), m.scroll)
	return header + "\n" + look.Faint(indexHint) + "\n\n" + view
}

func (m Model) indexList(width int, visible []Edit) string {
	if len(visible) == 0 {
		return look.Muted(noEdits)
	}
	key := listKey{width: width, count: len(visible), author: m.author, first: visible[0].ID, last: visible[len(visible)-1].ID, root: m.Root, plain: m.prefs.PlainPaths}
	if m.cache != nil && m.cache.listView != "" && m.cache.list == key {
		return m.cache.listView
	}
	cards := make([]string, 0, len(visible))
	for _, edit := range visible {
		word, _, colour := edit.op.mark()
		content := look.Sides(look.Style(colour).Bold(true).Render(strings.ToUpper(word))+"  "+look.Title(filepath.Base(edit.Path)), look.TypedID(editKind, trace.Short(edit.ID)), width-cardInset)
		content += "\n" + look.Faint(hyperlink(m.Root, edit.Path, m.prefs.PlainPaths)) + "\n" + edit.meta()
		cards = append(cards, look.TintedSurface(width-1, look.Panel, content))
	}
	list := strings.Join(cards, "\n\n")
	if m.cache != nil {
		m.cache.list, m.cache.listView = key, list
	}
	return list
}
