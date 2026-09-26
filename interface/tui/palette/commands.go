package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
)

type Commands struct{ list list[Item] }

func NewCommands(items []Item) Commands {
	return Commands{list: newList(func(query string) []Item {
		var shown []Item
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Title+" "+item.Description+" "+item.Key), query) {
				shown = append(shown, item)
			}
		}
		return shown
	})}
}

func (c *Commands) Key(msg tea.KeyPressMsg) (Choice, tea.Cmd) {
	item, done, cancelled, cmd := c.list.key(msg)
	return Choice{ID: item.ID, Done: done, Cancelled: cancelled}, cmd
}

func (c *Commands) Paste(msg tea.PasteMsg) tea.Cmd { return c.list.edit(msg) }

func (c *Commands) Click(x, y, width, height int) Choice {
	modal, start, labels := c.modal(width, height)
	x0, y0 := place(modal, width, height, dialogMargin)
	index, inside := rowAt(modal, x-x0, y-y0, modalRowLines, labels)
	if index < 0 {
		return Choice{Inside: inside}
	}
	c.list.cursor = start + index
	return Choice{ID: c.list.shown[c.list.cursor].ID, Done: true, Inside: true}
}

func (c Commands) Over(base string, width, height int) string {
	modal, _, _ := c.modal(width, height)
	x, y := place(modal, width, height, dialogMargin)
	return compose(base, modal, x, y)
}

func (c Commands) modal(width, height int) (modal string, start int, labels []string) {
	modalWidth := min(dialogMaxWidth, width-dialogInset)
	rowWidth := max(rowMinWidth, modalWidth-2*dialogPadding)
	c.list.input.SetWidth(rowWidth - filterInset)
	band := lipgloss.NewStyle().Width(rowWidth).Background(lipgloss.Color(string(look.PanelLight))).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color(string(look.Mint))).Render(c.list.input.View())
	start, end := look.VisibleRows(len(c.list.shown), c.list.cursor, max(minVisibleRows, (height-listChrome)/modalRowLines))
	var rows []string
	for i := start; i < end; i++ {
		item := c.list.shown[i]
		rows = append(rows, look.ModalRow(rowWidth, i == c.list.cursor, item.Title, item.Description, item.Key))
		labels = append(labels, item.Title)
	}
	if len(rows) == 0 {
		rows = []string{look.Muted("No matching actions")}
	}
	return look.ModalPane(modalWidth, growToContent, look.Panel, dialogPadding, heading("Commands", look.Muted("Jump to a view or run an action"), rowWidth)+band+"\n\n"+strings.Join(rows, "\n\n")), start, labels
}
