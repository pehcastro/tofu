package theme

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

const codespanPad = " "

func Prose() ansi.StyleConfig {
	flush, bold, italic := uint(0), true, true
	style := styles.DarkStyleConfig
	style.Document.Margin = &flush
	style.Document.Color = colour(ink)

	style.Heading.Color = colour(accentBlue)
	style.Heading.Bold = &bold
	style.H1.Color, style.H1.BackgroundColor = colour(speech), colour(accentBlue)
	style.H1.Bold = &bold
	style.H2.Prefix, style.H2.Color, style.H2.Bold = "", colour(accentBlue), &bold
	style.H3.Prefix, style.H3.Color, style.H3.Bold = "  ", colour(accentBlue), &bold
	style.H4.Prefix, style.H4.Color, style.H4.Bold = "    ", colour(muted), &bold
	style.H5.Prefix, style.H5.Color, style.H5.Bold = "      ", colour(muted), &bold
	style.H6.Prefix, style.H6.Color, style.H6.Bold = "        ", colour(faint), &bold

	style.Strong.Color, style.Strong.Bold = colour(speech), &bold
	style.Emph.Color, style.Emph.Italic = colour(muted), &italic

	style.Code.Prefix, style.Code.Suffix = codespanPad, codespanPad
	style.Code.Color, style.Code.BackgroundColor = colour(toolCyan), colour(panel)

	style.CodeBlock.Color = colour(muted)

	style.BlockQuote.Color = colour(muted)
	style.List.LevelIndent = 2
	style.Item.Color = colour(ink)
	style.Enumeration.Color = colour(ink)

	style.Link.Color = colour(accentBlue)
	style.LinkText.Color, style.LinkText.Bold = colour(accentBlue), &bold
	style.HorizontalRule.Color = colour(faint)

	style.Table.Color = colour(ink)
	style.DefinitionDescription.Color = colour(muted)
	return style
}

func colour(name string) *string { return &name }
