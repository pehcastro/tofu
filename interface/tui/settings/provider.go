package settings

import (
	"strings"

	"tofu/interface/tui/look"
	"tofu/internal/widget"
)

type Provider struct {
	Name   string
	State  string
	Key    string
	Fix    string
	Source string
}

func providerBlock(providers []Provider) string {
	block := look.SectionLabel("Providers")
	if len(providers) == 0 {
		return block + "\n" + look.Muted("no provider is set up yet")
	}
	for _, provider := range providers {
		var fields []string
		if provider.Key != "" {
			fields = append(fields, "key "+widget.Mask(provider.Key))
		}
		if provider.State != "" {
			fields = append(fields, provider.State)
		}
		value := strings.Join(fields, "  ")
		if value == "" {
			value = provider.Fix
		}
		block += "\n" + look.Title(provider.Name) + "  " + look.Muted(value)
		if provider.Source != "" {
			block += "\n" + look.Faint(provider.Source)
		}
	}
	return block
}
