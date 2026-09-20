package sift

import (
	"errors"
	"strings"
)

type Part struct {
	Text string
	Sep  string
}

func (p Part) Fenced() bool {
	return strings.HasPrefix(strings.TrimLeft(p.Text, " \t"), "```")
}

func (p Part) Words() int {
	return len(strings.Fields(p.Text))
}

var ErrAlreadySifted = errors.New("sift: the text already carries a sift marker or fence, so eliding it could not be undone")

func Split(text string) ([]Part, error) {
	var parts []Part
	var body, sep strings.Builder
	fenced := false
	for _, line := range lines(text) {
		if isMarkerLine(line) || isFence(line) {
			return nil, ErrAlreadySifted
		}
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
			fenced = !fenced
		}
		if !fenced && blank(line) {
			sep.WriteString(line)
			continue
		}
		if sep.Len() > 0 {
			if body.Len() > 0 {
				parts = append(parts, Part{Text: body.String(), Sep: sep.String()})
				body.Reset()
			} else {
				body.WriteString(sep.String())
			}
			sep.Reset()
		}
		body.WriteString(line)
	}
	if body.Len() > 0 || sep.Len() > 0 {
		parts = append(parts, Part{Text: body.String(), Sep: sep.String()})
	}
	return parts, nil
}

func Join(parts []Part) string {
	var out strings.Builder
	for _, p := range parts {
		out.WriteString(p.Text)
		out.WriteString(p.Sep)
	}
	return out.String()
}

func lines(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

func blank(line string) bool {
	return strings.TrimSpace(line) == ""
}
