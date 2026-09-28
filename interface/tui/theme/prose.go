package theme

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"

	"tofu/interface/tui/look"
)

func Prose() ansi.StyleConfig {
	flush, bold, italic, underline := uint(0), true, true, true
	style := styles.DarkStyleConfig
	style.Document.Margin = &flush
	style.Document.Color = colour(look.Text)

	style.Heading.Color, style.Heading.Bold = colour(look.Text), &bold
	style.H1.Color, style.H1.BackgroundColor, style.H1.Bold = colour(look.Text), colour(look.PanelLight), &bold
	style.H2.Prefix, style.H2.Color, style.H2.Bold = "", colour(look.Mint), &bold
	style.H3.Prefix, style.H3.Color, style.H3.Bold = "  ", colour(look.Blue), &bold
	style.H4.Prefix, style.H4.Color, style.H4.Bold = "    ", colour(look.MutedColor), &bold
	style.H5.Prefix, style.H5.Color, style.H5.Bold = "      ", colour(look.MutedColor), &bold
	style.H6.Prefix, style.H6.Color, style.H6.Bold = "        ", colour(look.FaintColor), &bold

	style.Strong.Color, style.Strong.Bold = colour(look.Text), &bold
	style.Emph.Color, style.Emph.Italic = colour(look.MutedColor), &italic

	style.Code.Prefix, style.Code.Suffix = "", ""
	style.Code.Color, style.Code.BackgroundColor = colour(look.Blue), nil

	style.CodeBlock.Color, style.CodeBlock.Margin = colour(look.MutedColor), &flush
	style.CodeBlock.Chroma = &ansi.Chroma{
		Text:                ansi.StylePrimitive{Color: colour(look.Text)},
		Error:               ansi.StylePrimitive{Color: colour(look.Red)},
		Comment:             ansi.StylePrimitive{Color: colour(look.SyntaxComment)},
		CommentPreproc:      ansi.StylePrimitive{Color: colour(look.SyntaxKeyword)},
		Keyword:             ansi.StylePrimitive{Color: colour(look.SyntaxKeyword)},
		KeywordReserved:     ansi.StylePrimitive{Color: colour(look.SyntaxKeyword)},
		KeywordNamespace:    ansi.StylePrimitive{Color: colour(look.SyntaxKeyword)},
		KeywordType:         ansi.StylePrimitive{Color: colour(look.SyntaxFunction)},
		Operator:            ansi.StylePrimitive{Color: colour(look.MutedColor)},
		Punctuation:         ansi.StylePrimitive{Color: colour(look.MutedColor)},
		Name:                ansi.StylePrimitive{Color: colour(look.Text)},
		NameBuiltin:         ansi.StylePrimitive{Color: colour(look.SyntaxFunction)},
		NameTag:             ansi.StylePrimitive{Color: colour(look.SyntaxKeyword)},
		NameAttribute:       ansi.StylePrimitive{Color: colour(look.SyntaxFunction)},
		NameClass:           ansi.StylePrimitive{Color: colour(look.SyntaxFunction), Bold: &bold},
		NameDecorator:       ansi.StylePrimitive{Color: colour(look.SyntaxKeyword)},
		NameFunction:        ansi.StylePrimitive{Color: colour(look.SyntaxFunction)},
		LiteralNumber:       ansi.StylePrimitive{Color: colour(look.SyntaxNumber)},
		LiteralString:       ansi.StylePrimitive{Color: colour(look.SyntaxString)},
		LiteralStringEscape: ansi.StylePrimitive{Color: colour(look.SyntaxNumber)},
		GenericDeleted:      ansi.StylePrimitive{Color: colour(look.Red)},
		GenericEmph:         ansi.StylePrimitive{Italic: &italic},
		GenericInserted:     ansi.StylePrimitive{Color: colour(look.Mint)},
		GenericStrong:       ansi.StylePrimitive{Bold: &bold},
		GenericSubheading:   ansi.StylePrimitive{Color: colour(look.MutedColor)},
	}

	style.BlockQuote.Color = colour(look.MutedColor)
	style.List.LevelIndent = 2
	style.Item.Color = colour(look.Text)
	style.Enumeration.Color = colour(look.Text)
	style.Task.Color = colour(look.Text)

	style.Link.Color, style.Link.Underline = colour(look.Blue), &underline
	style.LinkText.Color, style.LinkText.Bold = colour(look.Blue), &bold
	style.Image.Color, style.Image.Underline = colour(look.Blue), &underline
	style.ImageText.Color = colour(look.MutedColor)
	style.HorizontalRule.Color = colour(look.FaintColor)

	style.Table.Color = colour(look.Text)
	style.DefinitionDescription.Color = colour(look.MutedColor)
	return style
}

func colour(c look.Color) *string {
	value := string(c)
	return &value
}
