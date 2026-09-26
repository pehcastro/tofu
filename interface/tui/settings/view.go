package settings

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/internal/widget"
)

const (
	minimumWidth       = 60
	minimumHeight      = 12
	railMinWidth       = 22
	railMaxWidth       = 30
	railShare          = 5
	inspectorWidth     = 31
	inspectorFromWidth = 118
	paneGap            = 2
	panePad            = 2
	markerCells        = 2
	rowsTop            = 4
	categoriesTop      = 5
	footerLines        = 1
	previewMinHeight   = 30
	previewMinWidth    = 48
	previewInset       = 6
	choiceMaxWidth     = 54
	choiceMargin       = 6
	choiceChrome       = 9
	numberMaxWidth     = 72
	numberMaxDigits    = 12
	searchMaxWidth     = 68
	searchMargin       = 8
	searchMaxHeight    = 15
	searchInset        = 4
	searchMaxResults   = 7
	searchChrome       = 11
	searchResultsTop   = 6
	appearanceCategory = "Appearance"
	modelsCategory     = "Models & roles"
	densityCompact     = "compact"
	densitySpacious    = "spacious"
)

type viewKey struct {
	width, height, category, cursor, scope int
	scopes, searchKey, density, branch     string
	query                                  string
	dialog                                 choiceState
	search                                 searchState
	number                                 numberState
}

type viewCache struct {
	key       viewKey
	rows      []Row
	providers []Provider
	view      string
}

type layout struct{ rail, main, inspector int }

func (m *Model) View() string {
	key := viewKey{m.width, m.height, m.category, m.cursor, m.Scope, strings.Join(m.Scopes, " "), m.searchKey, m.density, m.branch, m.Query, m.dialog, m.search, m.number}
	if m.cache.view != "" && m.cache.key == key && slices.EqualFunc(m.cache.rows, m.Rows, sameRow) && slices.Equal(m.cache.providers, m.Providers) {
		return m.cache.view
	}
	view := m.render()
	m.cache = viewCache{key: key, rows: slices.Clone(m.Rows), providers: slices.Clone(m.Providers), view: view}
	return view
}

func sameRow(a, b Row) bool {
	return a.Key == b.Key && a.Category == b.Category && a.Label == b.Label && a.Description == b.Description &&
		a.Value == b.Value && a.Source == b.Source && a.Kind == b.Kind && a.Action == b.Action &&
		a.Number == b.Number && a.Changed == b.Changed && a.RestartRequired == b.RestartRequired && a.RestartPending == b.RestartPending &&
		slices.Equal(a.Choices, b.Choices)
}

func (m *Model) render() string {
	switch {
	case m.dialog.open:
		return overlay(m.body(), m.choiceDialog(), m.width, m.height)
	case m.search.open:
		return overlay(m.body(), m.searchDialog(), m.width, m.height)
	case m.number.open:
		return overlay(m.body(), m.numberDialog(), m.width, m.height)
	}
	return m.body()
}

func overlay(base, modal string, width, height int) string {
	x, y := centre(modal, width, height)
	return look.FixedBlock(width, height, look.Over(look.Dim(base), modal, x, y))
}

func centre(modal string, width, height int) (int, int) {
	return max(1, (width-lipgloss.Width(modal))/2), max(1, (height-lipgloss.Height(modal))/2)
}

func (m *Model) layout() layout {
	rail := min(railMaxWidth, max(railMinWidth, m.width/railShare))
	inspector := 0
	if m.width >= inspectorFromWidth {
		inspector = inspectorWidth
	}
	return layout{rail: rail, main: m.width - rail - inspector - paneGap, inspector: inspector}
}

func (m *Model) rowHeight() int {
	switch m.density {
	case densityCompact:
		return 2
	case densitySpacious:
		return 4
	}
	return 3
}

func (m *Model) visibleRows(count int) (start, end int) {
	return look.VisibleRows(count, m.cursor, max(1, (m.height-rowsTop-footerLines)/m.rowHeight()))
}

func (m *Model) searchHint(separator string) string {
	if m.searchKey == "" {
		return ""
	}
	return m.searchKey + " search" + separator
}

func (m *Model) body() string {
	geo := m.layout()
	scope := m.Scopes[m.Scope]
	names := m.categories()
	rail := look.Title("tofu settings") + "\n" + look.Muted(strings.ToUpper(scope[:1])+scope[1:]+" preferences") + "\n\n" + look.SectionLabel("Categories") + "\n"
	for i, name := range names {
		count := 0
		for _, row := range m.Rows {
			if row.Category == name {
				count++
			}
		}
		rail += look.SidebarItem(geo.rail-2*panePad, i == m.category, name, fmt.Sprintf("%02d", count)) + "\n"
	}
	rail += "\n" + look.SectionLabel("Navigate") + "\n" + look.Faint("↑↓ setting · ←→ section") + "\n" + look.Faint(m.searchHint(" · ")+"esc chat")
	title := "Settings"
	if len(names) > 0 {
		title = names[m.category]
	}
	width := geo.main - 2*panePad
	main := look.Sides(look.Title(title), look.Faint(scope+" scope"), width) + "\n" + look.Muted("Choose a row to inspect and change its value.") + "\n\n"
	indices := m.inCategory()
	start, end := m.visibleRows(len(indices))
	for i := start; i < end; i++ {
		main += m.settingRow(width, i == m.cursor, m.Rows[indices[i]]) + "\n\n"
	}
	if title == appearanceCategory && end-start == len(indices) && m.height >= previewMinHeight && geo.main >= previewMinWidth {
		main += look.SectionLabel("Live preview") + "\n" + m.preview(geo.main-previewInset) + "\n\n"
	}
	main += look.Faint("enter change  ·  " + m.searchHint("  ·  ") + "esc chat")
	view := look.JoinFixedPanes(look.Surface(geo.rail, m.height, look.Panel, panePad, "\n"+rail), look.Surface(geo.main, m.height, "", panePad, "\n"+main))
	if geo.inspector > 0 {
		view = look.JoinFixedPanes(view, look.Surface(geo.inspector, m.height, look.Panel, panePad, "\n"+m.inspector()))
	}
	return look.FixedBlock(m.width, m.height, view)
}

func (m *Model) settingRow(width int, selected bool, row Row) string {
	rendered := look.SettingRow(width, selected, row.Label, widget.Fit(row.Description, width-markerCells), m.display(row))
	switch m.density {
	case densityCompact:
		return strings.SplitN(rendered, "\n", 2)[0]
	case densitySpacious:
		return rendered + "\n"
	}
	return rendered
}

func (m *Model) display(row Row) string {
	switch row.Action {
	case RowKeybindings:
		return "edit"
	case RowHostIntegration:
		return strings.TrimSpace("inspect " + row.Value)
	case RowRole:
		return row.Value
	case RowValue:
	default:
		panic("settings: unknown row action")
	}
	switch {
	case m.dialog.open && m.dialog.key == row.Key:
		return row.Choices[m.dialog.cursor]
	case row.Kind == Bool && row.Value == "true":
		return "on"
	case row.Kind == Bool:
		return "off"
	case row.Value == "":
		return "default"
	}
	return row.Value
}

func (m *Model) inspector() string {
	row, ok := m.selected()
	if !ok {
		return look.SectionLabel("Selected setting")
	}
	value := m.display(row)
	scope := "Global · a project value overrides it"
	if m.Scope > 0 {
		scope = "Project · overrides the global value"
	}
	text := look.SectionLabel("Selected setting") + "\n\n" + look.Title(row.Label) + "\n" + look.Muted(row.Description) +
		"\n\n" + look.SectionLabel("Current value") + "\n" + look.StateBadge(value, value != "off") +
		"\n\n" + look.SectionLabel("How it works") + "\n" + look.Muted(howItWorks(row)) +
		"\n\n" + look.SectionLabel("Scope") + "\n" + look.Muted(scope) + "\n" + look.Faint("from "+row.Source)
	if row.Category == modelsCategory {
		text += "\n\n" + providerBlock(m.Providers)
	}
	return text
}

func howItWorks(row Row) string {
	var how string
	switch {
	case row.Action == RowKeybindings:
		how = "Enter opens the shortcut editor. A changed shortcut marks the host keymap as needing an update."
	case row.Action == RowHostIntegration:
		how = "Inspect tofu shortcuts and host conflicts. Apply previews terminal-only rules, then confirms a backed-up change. Undo removes tofu rules."
	case row.Action == RowRole:
		how = "Enter opens the model picker and binds the choice to this role."
	case row.Kind == Bool:
		how = "Enter or Space switches it on or off and saves it."
	case row.Kind == Int:
		how = "Enter opens an input for a typed value. + raises it, - lowers it, and each step saves."
	case len(row.Choices) > 0:
		how = "Enter opens a focused choice. Changes preview immediately; Esc keeps the prior value."
	default:
		how = "Set it in the file named under Scope. This screen shows it and does not edit it."
	}
	switch {
	case row.RestartPending:
		how += " A restart is pending to apply it."
	case row.RestartRequired:
		how += " Takes effect after a restart."
	}
	return how
}

func (m *Model) preview(width int) string {
	place := "project"
	if m.branch != "" {
		place += "  ·  " + m.branch
	}
	return look.TintedSurface(width, look.PanelLight,
		look.Painted(" tofu ", look.Background, look.Mint)+look.Faint("   "+place)+"\n"+
			look.Title("Interface preview")+"\n"+
			look.Muted("Readable text, quiet surfaces, and bounded focus.")+"\n"+
			look.Accent("● active")+look.Faint("    ")+look.Style(look.Amber).Render("● attention")+look.Faint("    ")+look.Style(look.Red).Render("● failed"))
}

func (m *Model) choiceWindow(row Row) (start, end int) {
	return look.VisibleRows(len(row.Choices), m.dialog.cursor, min(len(row.Choices), max(1, (m.height-choiceChrome)/2)))
}

func (m *Model) choiceDialog() string {
	row, _ := m.selected()
	start, end := m.choiceWindow(row)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, look.DialogChoice(i == m.dialog.cursor, row.Choices[i]))
	}
	footer := fmt.Sprintf("%d-%d/%d · ↑↓ preview · enter apply · esc cancel", start+1, end, len(row.Choices))
	return look.DialogPanel(min(choiceMaxWidth, m.width-choiceMargin), row.Label, row.Description, strings.Join(lines, "\n\n"), footer)
}

func (m *Model) numberDialog() string {
	row, _ := m.selected()
	body := look.Muted("Current  ") + look.Title(row.Value) + "\n" +
		look.Muted("Default  ") + look.Title(row.Number.DefaultMeaning) + "\n" +
		look.Muted("Range    ") + look.Title(fmt.Sprintf("%d to %d", row.Number.Least, row.Number.Most)) +
		"\n\n" + look.Accent("› ") + look.Title(m.number.typed) + look.Accent("█") + "\n" + look.Style(look.Red).Render(m.number.refusal)
	return look.DialogPanel(min(numberMaxWidth, m.width-choiceMargin), row.Label, row.Description, body, "type digits · enter save · esc keep "+row.Value)
}

func (m *Model) searchWindow(found []int) (start, end int) {
	return look.VisibleRows(len(found), m.search.cursor, min(searchMaxResults, max(2, m.height-searchChrome)))
}

func resultLabel(row Row) string { return row.Category + " / " + row.Label }

func (m *Model) searchDialog() string {
	width := min(searchMaxWidth, m.width-searchMargin)
	inner := width - 2*panePad
	content := look.Sides(look.Title("Search settings"), look.Faint("esc"), inner) + "\n" + look.Faint("Type to filter · ↑↓ select · enter jump") +
		"\n\n" + look.Accent("› ") + look.Title(m.Query) + look.Accent("█") + "\n\n"
	found := m.matches()
	if len(found) == 0 {
		content += look.Muted("No matching settings")
	} else {
		start, end := m.searchWindow(found)
		for i := start; i < end; i++ {
			content += look.CatalogRow(inner, i == m.search.cursor, resultLabel(m.Rows[found[i]])) + "\n"
		}
		content += "\n" + look.Faint(fmt.Sprintf("%d of %d results", m.search.cursor+1, len(found)))
	}
	return look.ModalPane(width, min(m.height-searchInset, searchMaxHeight), look.Panel, panePad, content)
}
