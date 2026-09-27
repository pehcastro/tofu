package markdown

import (
	"strings"

	"charm.land/glamour/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

type Renderer struct {
	width int
	term  *glamour.TermRenderer
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
	styled, err := r.term.Render(tagBareFences(source))
	if err != nil {
		return widget.Wrap(source, width)
	}
	lines := strings.Split(styled, "\n")
	for index, line := range lines {
		lines[index] = trimRight(ansi.Truncate(line, width, ""))
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func tagBareFences(source string) string {
	lines := strings.Split(source, "\n")
	for open := 0; open < len(lines); open++ {
		if !isFenceLine(lines[open]) {
			continue
		}
		end := open + 1
		for end < len(lines) && !isFenceLine(lines[end]) {
			end++
		}
		if strings.TrimLeft(lines[open], " `~") == "" {
			lines[open] += guessLanguage(strings.Join(lines[open+1:end], "\n"))
		}
		open = end
	}
	return strings.Join(lines, "\n")
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
