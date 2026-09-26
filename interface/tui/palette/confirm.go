package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/look"
)

type Confirm struct {
	title, subtitle, subject string
	options                  []Item
	cursor                   int
}

func NewConfirm(title, subtitle, subject string, options []Item) Confirm {
	return Confirm{title: title, subtitle: subtitle, subject: subject, options: options}
}

func (c *Confirm) Key(msg tea.KeyPressMsg) Choice {
	switch msg.String() {
	case "esc":
		return Choice{Cancelled: true}
	case "up":
		c.cursor = wrap(c.cursor-1, len(c.options))
	case "down":
		c.cursor = wrap(c.cursor+1, len(c.options))
	case "enter":
		return Choice{ID: c.options[c.cursor].ID, Done: true}
	}
	return Choice{}
}

func (c *Confirm) Click(x, y, width, height int) Choice {
	modal := c.modal(width)
	x0, y0 := place(modal, width, height, dialogMargin)
	labels := make([]string, len(c.options))
	for i, option := range c.options {
		labels[i] = option.Title
	}
	index, inside := rowAt(modal, x-x0, y-y0, modalRowLines, labels)
	if index < 0 {
		return Choice{Inside: inside}
	}
	c.cursor = index
	return Choice{ID: c.options[index].ID, Done: true, Inside: true}
}

func (c Confirm) Over(base string, width, height int) string {
	modal := c.modal(width)
	x, y := place(modal, width, height, dialogMargin)
	return compose(base, modal, x, y)
}

func (c Confirm) modal(width int) string {
	modalWidth := min(confirmMaxWidth, width-dialogInset)
	rowWidth := max(rowMinWidth, modalWidth-2*dialogPadding)
	rows := []string{look.QuietBadge(c.subject)}
	for i, option := range c.options {
		rows = append(rows, look.ModalRow(rowWidth, i == c.cursor, option.Title, option.Description, option.Key))
	}
	return look.ModalPane(modalWidth, growToContent, look.Panel, dialogPadding, heading(c.title, look.Muted(c.subtitle), rowWidth)+strings.Join(rows, "\n\n"))
}
