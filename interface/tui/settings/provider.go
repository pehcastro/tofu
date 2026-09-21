package settings

import (
	"strings"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

type Provider struct {
	Name   string
	State  string
	Key    string
	Fix    string
	Source string
}

func providerLines(providers []Provider, width int) []string {
	if len(providers) == 0 {
		return []string{theme.Dim().Render(widget.Fit(indent+"no provider is set up yet", width))}
	}
	var lines []string
	for index, provider := range providers {
		label := ""
		if index == 0 {
			label = "providers"
		}
		lines = append(lines, providerRow(label, provider, width)...)
	}
	return lines
}

func providerRow(label string, provider Provider, width int) []string {
	head := indent + pad(label, groupColumn) + pad(provider.Name, nameColumn-groupColumn) + providerValue(provider)
	source := sourceMark + provider.Source
	if gap, fits := fitsWithSource(head, source, width); fits {
		return []string{theme.Text().Render(head) + theme.Faint().Render(gap+source)}
	}
	return []string{
		theme.Text().Render(widget.Fit(head, width)),
		theme.Faint().Render(widget.Fit(indent+pad("", groupColumn)+source, width)),
	}
}

func providerValue(provider Provider) string {
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
