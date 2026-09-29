package models

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/internal/llm"
	"tofu/internal/widget"
)

const (
	narrowDialog    = 88
	narrowSide      = 23
	wideSide        = 24
	detailMin       = 28
	detailMax       = 32
	dialogMargin    = 4
	dialogMinWidth  = 100
	dialogMinHeight = 19
	pageChrome      = 9
	pageLineHeight  = 19
	filterMinWidth  = 16
	filterInset     = 5
	rowInset        = 4
	sideInset       = 2
	fieldColumn     = 8
	gap             = "  "
	dialogTitle     = "Select model"
	providersHead   = "Providers"
	catalogHead     = "Catalog"
	catalogHint     = "←→ provider · tab roles"
	pageHint        = "  ·  ↑↓ models"
	noMatch         = "No matching models"
	emptyTitle      = "the library has no model to pick"
	excludedMark    = " · excluded"
	rolesHead       = "Role presets"
	rolesHint       = "enter assign · tab"
	selectionHead   = "Selection"
	excludedHead    = "Excluded"
	fromHead        = "From"
	noticeHead      = "Notice"
	reloadHint      = "f5 reloads models"
	reloadingHint   = "reloading models"
	effortHead      = "Effort"
	effortHint      = "  shift+←→"
	assignHead      = "Assign to"
	boundHead       = "Assigned model"
	noSelection     = "No model selected"
	noRoles         = "no role or sub-agent to assign"
	pickHint        = "enter picks the model"
	bindHint        = "enter assigns the model"
	loginHint       = "enter starts the login"
	rebindHint      = "enter chooses a new model"
	keyHint         = "enter asks for the key"
	keySetMark      = " · key set"
	noKeyMark       = " · no key"
	keyInset        = 1
	keyPanelPad     = 2
	keyBoxChrome    = 4
	keyTitle        = " key"
	keyFor          = "for "
	keyStored       = "stored in the credential store, never shown"
	keyHints        = "enter save · esc cancel"
	keyChecking     = "checking with "
	checkingHints   = "esc cancel"
	promptGlyph     = "› "
	caretGlyph      = "█"
	maskGlyph       = "•"
)

type renderKey struct {
	width, height    int
	tab              tab
	provider, cursor int
	filter, bound    string
	assign           *Target
	effort           llm.Effort
	entry            keyEntry
	reloading        bool
}

type drawn struct {
	key   renderKey
	modal string
}

func modelPaneWidths(modalWidth int) (side, list, detail int) {
	if modalWidth < narrowDialog {
		return narrowSide, modalWidth - narrowSide, 0
	}
	detail = min(detailMax, max(detailMin, modalWidth/4))
	return wideSide, modalWidth - wideSide - detail, detail
}

func modelDialogSize(width, height int) (int, int) {
	return max(1, min(width-dialogMargin, max(dialogMinWidth, width*2/3))), max(1, min(height-dialogMargin, max(dialogMinHeight, height*2/3)))
}

func modelPageSize(modalHeight int) int {
	return max(1, (modalHeight-pageChrome)/linesPerRow)
}

func (m Model) pageSize() int {
	_, modalHeight := modelDialogSize(m.width, m.height)
	return modelPageSize(modalHeight)
}

func (m Model) page() (start, end int) {
	size := m.pageSize()
	start = m.cursor / size * size
	return start, min(m.count(), start+size)
}

func origin(width, height int) (int, int) {
	modalWidth, modalHeight := modelDialogSize(width, height)
	return max(1, (width-modalWidth)/2), max(1, (height-modalHeight)/2)
}

func (m Model) View() string {
	blank := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", m.width)+"\n", max(m.height, 1)), "\n")
	return m.Dialog(blank, m.width, m.height)
}

func (m Model) Dialog(base string, width, height int) string {
	left, top := origin(width, height)
	return look.Over(look.Dim(base), m.cached(width, height), left, top)
}

func (m Model) cached(width, height int) string {
	bound := make([]string, 0, len(m.targets))
	for _, target := range m.targets {
		bound = append(bound, target.Assigned)
	}
	key := renderKey{width: width, height: height, tab: m.tab, provider: m.provider, cursor: m.cursor,
		filter: m.filter.Value(), bound: strings.Join(bound, "\n"), assign: m.assign, effort: m.effort, entry: m.entry, reloading: m.reloading}
	if m.drawn.modal == "" || m.drawn.key != key {
		*m.drawn = drawn{key: key, modal: m.modal(width, height)}
	}
	return m.drawn.modal
}

func (m Model) keyDialog(width int) string {
	inner := width - 2*keyPanelPad
	input, hints := look.Muted(keyChecking+m.entry.provider), checkingHints
	if m.entry.checking == 0 {
		shown := min(utf8.RuneCountInString(m.entry.typed), inner-keyBoxChrome-utf8.RuneCountInString(promptGlyph+caretGlyph))
		input, hints = look.Accent(promptGlyph)+look.Title(strings.Repeat(maskGlyph, max(0, shown)))+look.Accent(caretGlyph), keyHints
	}
	body := lipgloss.NewStyle().Width(inner).Padding(0, 1).Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(string(look.FaintColor))).Render(input)
	if m.entry.refusal != "" {
		body += "\n" + look.Style(look.Red).Width(inner).Render(m.entry.refusal)
	}
	body += "\n\n" + look.Muted(keyStored)
	return look.DialogPanel(width, m.entry.provider+keyTitle, keyFor+m.entry.slug, body, hints)
}

func (m Model) modal(width, height int) string {
	modalWidth, modalHeight := modelDialogSize(width, height)
	side, list, detail := modelPaneWidths(modalWidth)
	panes := []string{
		look.ModalPane(side, modalHeight, look.Panel, 1, m.sidePane(side)),
		look.ModalPane(list, modalHeight, look.PanelLight, 1, m.listPane(list, modalHeight)),
	}
	if detail > 0 {
		panes = append(panes, look.ModalPane(detail, modalHeight, look.Panel, 1, m.detailPane()))
	}
	modal := lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	if m.entry.name != "" {
		dialog := m.keyDialog(list - 2*keyInset)
		modal = look.Over(look.Dim(modal), dialog, side+keyInset, max(0, (modalHeight-lipgloss.Height(dialog))/2))
	}
	return lipgloss.NewStyle().MaxHeight(modalHeight).Render(modal)
}

func (m Model) sidePane(width int) string {
	tabs := look.Accent(modelsTab) + gap + look.Muted(rolesTab)
	if m.tab == tabRoles {
		tabs = look.Muted(modelsTab) + gap + look.Accent(rolesTab)
	}
	total := 0
	for _, group := range m.groups() {
		total += len(group.Rows)
	}
	view := look.Title(dialogTitle) + "\n" + look.Faint(strconv.Itoa(total)+" models") + "\n\n" + tabs + "\n\n" + look.SectionLabel(providersHead) + "\n"
	for i, source := range m.providers() {
		view += look.CatalogRow(width-sideInset, i == m.provider && m.tab == tabModels, source) + "\n"
	}
	hint := reloadHint
	if m.reloading {
		hint = reloadingHint
	}
	return view + "\n" + look.Faint(hint)
}

func (m Model) listPane(width, modalHeight int) string {
	start, end := m.page()
	if m.tab == tabRoles {
		view := look.SectionLabel(rolesHead) + "\n" + look.Faint(rolesHint) + "\n\n"
		for i, target := range m.targets[start:end] {
			view += look.CatalogRow(width-rowInset, start+i == m.cursor, target.Name) + "\n  " + look.Faint(widget.Fit(target.Job, width-rowInset)) + "\n"
		}
		if len(m.targets) == 0 {
			view += look.Muted(noRoles)
		}
		return view
	}
	filter := m.filter
	filter.SetWidth(max(filterMinWidth, width-filterInset))
	view := look.SectionLabel(catalogHead) + "\n" + look.Faint(catalogHint) + "\n\n" + filter.View() + "\n\n"
	rows := m.visible()
	for i, row := range rows[start:end] {
		source, model := row.name()
		if row.excluded() {
			source += excludedMark
		}
		view += look.CatalogRow(width-rowInset, start+i == m.cursor, model) + "\n  " + look.Faint(source) + "\n"
	}
	size := m.pageSize()
	if len(rows) > size && modalHeight >= pageLineHeight {
		view += "\n" + look.Faint("Page "+strconv.Itoa(start/size+1)+"/"+strconv.Itoa((len(rows)+size-1)/size)+pageHint)
	}
	switch {
	case len(m.groups()) == 0:
		view += look.Muted(emptyTitle)
	case len(rows) == 0:
		view += look.Muted(noMatch)
	}
	return view
}

func field(label, value string) string {
	return look.Faint(widget.Pad(label, fieldColumn)) + look.Muted(value) + "\n"
}

func (m Model) detailPane() string {
	view := look.SectionLabel(selectionHead) + "\n\n"
	if m.tab == tabRoles {
		if m.cursor >= len(m.targets) {
			return view + look.Muted(noRoles)
		}
		target := m.targets[m.cursor]
		assigned := target.Assigned
		if assigned == "" && target.Role != "" {
			assigned = target.Role.Unbound()
		}
		return view + look.Title(target.Name) + "\n" + look.Muted(target.Job) + "\n\n" + look.SectionLabel(boundHead) + "\n" + look.Muted(assigned) + "\n\n" + look.Faint(rebindHint)
	}
	row, picked := m.Picked()
	if !picked {
		return view + look.Muted(noSelection)
	}
	source, model := row.name()
	if row.Label != "" {
		view += look.Title(model) + "\n" + look.Muted(source) + "\n"
	} else {
		view += look.Title(model) + "\n" + look.Muted(row.Slug) + "\n\n" +
			field("source", source) + field("kind", string(row.Kind)) + field("pays", string(row.Pays)) + field("window", row.Window) + field("layer", row.Layer)
	}
	switch group := m.groupOf(row); {
	case group.Key == "":
	case group.KeySet:
		view += field("key", "set")
	default:
		view += field("key", "not set")
	}
	if row.Notice != "" {
		view += "\n" + look.SectionLabel(noticeHead) + "\n" + look.Muted(row.Notice) + "\n"
	}
	if row.From != "" {
		view += "\n" + look.SectionLabel(fromHead) + "\n" + look.Muted(row.From) + "\n"
	}
	hint := pickHint
	if m.effort != "" && m.assign == nil {
		view += "\n" + look.SectionLabel(effortHead) + "\n" + look.Title(string(m.effort)) + look.Faint(effortHint) + "\n"
	}
	if m.assign != nil {
		view += "\n" + look.SectionLabel(assignHead) + "\n" + look.Title(m.assign.Name) + "\n"
		hint = bindHint
	}
	if row.excluded() {
		view += "\n" + look.SectionLabel(excludedHead) + "\n" + look.Muted(row.Reason) + "\n"
		hint = loginHint
	}
	if m.missingKey(row) != "" {
		hint = keyHint
	}
	return view + "\n" + look.Faint(hint)
}
