package markdown

import (
	"strings"

	"charm.land/glamour/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	codeRule   = "│ "
	codeIndent = "    "
)

type Renderer struct {
	width int
	term  *glamour.TermRenderer
}

type block struct {
	text string
	code bool
}

func (r *Renderer) Lines(source string, width int) []string {
	if width <= 0 {
		return nil
	}
	if r.term == nil || r.width != width {
		term, err := glamour.NewTermRenderer(glamour.WithStyles(theme.Prose()), glamour.WithWordWrap(width), glamour.WithChromaFormatter("terminal16m"))
		if err != nil {
			return widget.Wrap(source, width)
		}
		r.width, r.term = width, term
	}
	rule := look.Style(look.FaintColor).Render(codeRule)
	var lines []string
	for _, part := range blocks(source) {
		styled, err := r.term.Render(part.text)
		if err != nil {
			return widget.Wrap(source, width)
		}
		rendered := strings.Split(styled, "\n")
		for len(rendered) > 0 && strings.TrimSpace(ansi.Strip(rendered[0])) == "" {
			rendered = rendered[1:]
		}
		for len(rendered) > 0 && strings.TrimSpace(ansi.Strip(rendered[len(rendered)-1])) == "" {
			rendered = rendered[:len(rendered)-1]
		}
		if len(rendered) > 0 && len(lines) > 0 {
			lines = append(lines, "")
		}
		for _, line := range rendered {
			if part.code {
				line = rule + line
			}
			lines = append(lines, trimRight(ansi.Truncate(line, width, "")))
		}
	}
	return lines
}

func blocks(source string) []block {
	var parts []block
	var prose []string
	flushProse := func() {
		if text := strings.Join(prose, "\n"); strings.TrimSpace(text) != "" {
			parts = append(parts, block{text: text})
		}
		prose = nil
	}
	lines := strings.Split(source, "\n")
	for at := 0; at < len(lines); at++ {
		var code []string
		language := ""
		switch {
		case isFenceLine(lines[at]):
			end := at + 1
			for end < len(lines) && !isFenceLine(lines[end]) {
				end++
			}
			code, language, at = lines[at+1:min(end, len(lines))], strings.TrimLeft(lines[at], " `~"), end
		case opensIndentedCode(lines[at], prose):
			for ; at < len(lines) && (strings.TrimSpace(lines[at]) == "" || indented(lines[at])); at++ {
				code = append(code, strings.TrimPrefix(strings.TrimPrefix(lines[at], "\t"), codeIndent))
			}
			at--
			for strings.TrimSpace(code[len(code)-1]) == "" {
				code = code[:len(code)-1]
			}
		default:
			prose = append(prose, lines[at])
			continue
		}
		flushProse()
		body := strings.Join(code, "\n")
		if language == "" {
			language = guessLanguage(body)
		}
		parts = append(parts, block{text: "```" + language + "\n" + body + "\n```", code: true})
	}
	flushProse()
	return parts
}

func indented(line string) bool {
	return strings.HasPrefix(line, codeIndent) || strings.HasPrefix(line, "\t")
}

func opensIndentedCode(line string, prose []string) bool {
	if !indented(line) || strings.TrimSpace(line) == "" || (len(prose) > 0 && strings.TrimSpace(prose[len(prose)-1]) != "") {
		return false
	}
	for index := len(prose) - 1; index >= 0; index-- {
		if strings.TrimSpace(prose[index]) != "" {
			return !opensConstruct(prose[index])
		}
	}
	return true
}

func guessLanguage(code string) string {
	if strings.HasPrefix(strings.TrimSpace(code), "$ ") {
		return "console"
	}
	if lexer := lexers.Analyse(code); lexer != nil && len(lexer.Config().Aliases) > 0 {
		return lexer.Config().Aliases[0]
	}
	return ""
}

func trimRight(line string) string {
	state, read, kept := byte(0), 0, 0
	for read < len(line) {
		seq, width, size, next := ansi.DecodeSequence(line[read:], state, nil)
		read, state = read+size, next
		if width > 0 && strings.TrimSpace(seq) != "" {
			kept = read
		}
	}
	if kept == 0 {
		return ""
	}
	if kept < len(line) {
		return line[:kept] + ansi.ResetStyle
	}
	return line
}
