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
	codeRule       = "│ "
	codeIndent     = "    "
	hyperlinkStart = "\x1b]8;"
)

type Renderer struct {
	width int
	term  *glamour.TermRenderer
}

type blockKind int

const (
	proseBlock blockKind = iota
	codeBlock
	tableBlock
)

type block struct {
	text string
	kind blockKind
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
		var rendered []string
		var err error
		switch part.kind {
		case tableBlock:
			rendered, err = r.table(part.text, width)
		case proseBlock, codeBlock:
			rendered, err = r.render(part.text)
		}
		if err != nil {
			return widget.Wrap(source, width)
		}
		if len(rendered) > 0 && len(lines) > 0 {
			lines = append(lines, "")
		}
		for _, line := range rendered {
			if part.kind == codeBlock {
				line = rule + line
			}
			lines = append(lines, trimRight(ansi.Truncate(line, width, "")))
		}
	}
	return lines
}

func (r *Renderer) render(text string) ([]string, error) {
	styled, err := r.term.Render(text)
	if err != nil {
		return nil, err
	}
	rendered := strings.Split(styled, "\n")
	for len(rendered) > 0 && strings.TrimSpace(ansi.Strip(rendered[0])) == "" {
		rendered = rendered[1:]
	}
	for len(rendered) > 0 && strings.TrimSpace(ansi.Strip(rendered[len(rendered)-1])) == "" {
		rendered = rendered[:len(rendered)-1]
	}
	return rendered, nil
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
		case opensTable(lines, at):
			end := at + 2
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" && strings.Contains(lines[end], "|") {
				end++
			}
			flushProse()
			parts = append(parts, block{text: strings.Join(lines[at:end], "\n"), kind: tableBlock})
			at = end - 1
			continue
		default:
			prose = append(prose, lines[at])
			continue
		}
		flushProse()
		body := strings.Join(code, "\n")
		if language == "" {
			language = guessLanguage(body)
		}
		parts = append(parts, block{text: "```" + language + "\n" + body + "\n```", kind: codeBlock})
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
	linked, linkedAtKept := false, false
	for read < len(line) {
		seq, width, size, next := ansi.DecodeSequence(line[read:], state, nil)
		read, state = read+size, next
		if link, isLink := strings.CutPrefix(seq, hyperlinkStart); isLink {
			_, target, _ := strings.Cut(link, ";")
			linked = strings.TrimRight(target, "\a\x1b\\") != ""
		}
		if width > 0 && strings.TrimSpace(seq) != "" {
			kept, linkedAtKept = read, linked
		}
	}
	if kept == 0 {
		return ""
	}
	trimmed := line[:kept]
	if kept < len(line) {
		trimmed += ansi.ResetStyle
	}
	if linkedAtKept {
		trimmed += ansi.ResetHyperlink()
	}
	return trimmed
}
