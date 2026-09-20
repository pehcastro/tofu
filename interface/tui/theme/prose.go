package theme

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

func Prose() ansi.StyleConfig {
	flush, bold := uint(0), true
	style := styles.DarkStyleConfig
	style.Document.Margin = &flush
	style.Document.Color = colour(ink)
	style.Heading.Color = colour(accentBlue)
	style.Strong.Color, style.Strong.Bold = colour(speech), &bold
	style.Emph.Color = colour(muted)
	style.Code.Color, style.Code.BackgroundColor = colour(toolCyan), colour(panel)
	style.Link.Color = colour(accentBlue)
	style.HorizontalRule.Color = colour(faint)
	return style
}

func colour(name string) *string { return &name }
