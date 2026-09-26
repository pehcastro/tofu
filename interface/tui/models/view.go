package models

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
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
	rolesHead       = "Roles"
	rolesHint       = "enter binds · tab models"
	selectionHead   = "Selection"
	excludedHead    = "Excluded"
	effortHead      = "Effort"
	effortHint      = "  shift+←→"
	assignHead      = "Assign to"
	boundHead       = "Bound model"
	nothingBound    = "nothing bound, so the subscription default runs"
	noSelection     = "No model selected"
	pickHint        = "enter picks the model"
	bindHint        = "enter binds the model"
	loginHint       = "enter starts the login"
	rebindHint      = "enter chooses a new model"
)

type renderKey struct {
	width, height    int
	tab              tab
	provider, cursor int
	filter, bound    string
	assign           library.RoleID
	effort           llm.Effort
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
	return start, min(len(m.visible()), start+size)
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
	bound := make([]string, 0, len(library.RoleIDs()))
	for _, role := range library.RoleIDs() {
		bound = append(bound, m.bound[role])
	}
	key := renderKey{width: width, height: height, tab: m.tab, provider: m.provider, cursor: m.cursor,
		filter: m.filter.Value(), bound: strings.Join(bound, "\n"), assign: m.assign, effort: m.effort}
	if m.drawn.modal == "" || m.drawn.key != key {
		*m.drawn = drawn{key: key, modal: m.modal(width, height)}
	}
	return m.drawn.modal
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
	return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

func (m Model) sidePane(width int) string {
	tabs := look.Accent(modelsTab) + gap + look.Muted(rolesTab)
	if m.tab == tabRoles {
		tabs = look.Muted(modelsTab) + gap + look.Accent(rolesTab)
	}
	total := 0
	for _, group := range m.Groups {
		total += len(group.Rows)
	}
	view := look.Title(dialogTitle) + "\n" + look.Faint(strconv.Itoa(total)+" models") + "\n\n" + tabs + "\n\n" + look.SectionLabel(providersHead) + "\n"
	for i, source := range m.providers() {
		view += look.CatalogRow(width-sideInset, i == m.provider && m.tab == tabModels, source) + "\n"
	}
	return view
}

func (m Model) listPane(width, modalHeight int) string {
	if m.tab == tabRoles {
		view := look.SectionLabel(rolesHead) + "\n" + look.Faint(rolesHint) + "\n\n"
		for i, role := range library.RoleIDs() {
			view += look.CatalogRow(width-rowInset, i == m.cursor, string(role)) + "\n  " + look.Faint(role.What()) + "\n"
		}
		return view
	}
	filter := m.filter
	filter.SetWidth(max(filterMinWidth, width-filterInset))
	view := look.SectionLabel(catalogHead) + "\n" + look.Faint(catalogHint) + "\n\n" + filter.View() + "\n\n"
	rows := m.visible()
	start, end := m.page()
	for i, row := range rows[start:end] {
		source, model, _ := strings.Cut(row.Slug, "/")
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
	case len(m.Groups) == 0:
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
		role := library.RoleIDs()[m.cursor]
		bound := m.bound[role]
		if bound == "" {
			bound = nothingBound
		}
		return view + look.Title(string(role)) + "\n" + look.Muted(role.What()) + "\n\n" + look.SectionLabel(boundHead) + "\n" + look.Muted(bound) + "\n\n" + look.Faint(rebindHint)
	}
	row, picked := m.Picked()
	if !picked {
		return view + look.Muted(noSelection)
	}
	source, model, _ := strings.Cut(row.Slug, "/")
	view += look.Title(model) + "\n" + look.Muted(row.Slug) + "\n\n" +
		field("source", source) + field("kind", string(row.Kind)) + field("pays", string(row.Pays)) + field("window", row.Window)
	hint := pickHint
	if m.effort != "" {
		view += "\n" + look.SectionLabel(effortHead) + "\n" + look.Title(string(m.effort)) + look.Faint(effortHint) + "\n"
	}
	if m.assign != "" {
		view += "\n" + look.SectionLabel(assignHead) + "\n" + look.Title(string(m.assign)) + "\n"
		hint = bindHint
	}
	if row.excluded() {
		view += "\n" + look.SectionLabel(excludedHead) + "\n" + look.Muted(row.Reason) + "\n"
		hint = loginHint
	}
	return view + "\n" + look.Faint(hint)
}
