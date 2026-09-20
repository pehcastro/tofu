package settings

import (
	"strings"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	groupColumn  = 12
	nameColumn   = 14
	sourceMark   = "← "
	sourceGap    = 2
	indent       = "  "
	minimumWidth = 20
)

type Provider struct {
	Name   string
	State  string
	Key    string
	Fix    string
	Source string
}

type Model struct {
	Providers []Provider
	width     int
	height    int
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m Model) View() string {
	lines := []string{theme.Accent().Render(widget.Fit("settings", m.width)), ""}
	if len(m.Providers) == 0 {
		lines = append(lines, theme.Dim().Render(widget.Fit(indent+"no provider is set up yet", m.width)))
	}
	for index, provider := range m.Providers {
		label := ""
		if index == 0 {
			label = "providers"
		}
		lines = append(lines, m.row(label, provider)...)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) row(label string, provider Provider) []string {
	head := indent + pad(label, groupColumn) + pad(provider.Name, nameColumn) + value(provider)
	source := sourceMark + provider.Source
	if widget.Cells(head)+sourceGap+widget.Cells(source) <= m.width {
		gap := strings.Repeat(" ", m.width-widget.Cells(head)-widget.Cells(source))
		return []string{theme.Text().Render(head) + theme.Faint().Render(gap+source)}
	}
	return []string{
		theme.Text().Render(widget.Fit(head, m.width)),
		theme.Faint().Render(widget.Fit(indent+pad("", groupColumn)+source, m.width)),
	}
}

func value(provider Provider) string {
	var fields []string
	if provider.Key != "" {
		fields = append(fields, "key "+widget.Mask(provider.Key))
	}
	if provider.State != "" {
		fields = append(fields, provider.State)
	}
	if len(fields) == 0 {
		return provider.Fix
	}
	return strings.Join(fields, "  ")
}

func pad(text string, width int) string {
	if widget.Cells(text) >= width {
		return text + " "
	}
	return text + strings.Repeat(" ", width-widget.Cells(text))
}
