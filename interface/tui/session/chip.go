package session

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/paste"
	"tofu/internal/widget"
)

const (
	textChipThreshold = 160
	textTokenTail     = " characters]"
	treeBranch        = "├─ "
	treeLast          = "└─ "
)

type ChipKind int

const (
	ImageChip ChipKind = iota
	TextChip
)

type Chip struct {
	Kind   ChipKind
	Name   string
	Format string
	Bytes  int
	Chars  int
	Token  string
	Text   string
}

type pendingPaste struct {
	index int
	token string
}

func pastingToken(index int) string { return pastingTokenHead + strconv.Itoa(index) + "]" }

func ImageToken(index int) string { return imageTokenHead + strconv.Itoa(index) + "]" }

func textToken(chars int) string { return textTokenHead + strconv.Itoa(chars) + textTokenTail }

func (m *Model) Paste(board paste.Board) tea.Cmd {
	m.pastes++
	token := pastingToken(m.pastes)
	m.composer.InsertString(token)
	m.pending = append(m.pending, pendingPaste{index: m.pastes, token: token})
	return board.Attach(m.pastes)
}

func (m *Model) Attached(outcome paste.Outcome) {
	at := slices.IndexFunc(m.pending, func(row pendingPaste) bool { return row.index == outcome.Index })
	if at < 0 {
		return
	}
	held := m.pending[at]
	m.pending = slices.Delete(m.pending, at, at+1)
	switch outcome.State {
	case paste.Ready:
		token := ImageToken(outcome.Index)
		m.replaceToken(held.token, token)
		m.chips = append(m.chips, Chip{Kind: ImageChip, Name: outcome.Name, Format: outcome.Format(), Bytes: outcome.Bytes, Token: token})
	case paste.Textual:
		m.replaceToken(held.token, m.textOrChip(outcome.Text))
	case paste.Failed:
		m.replaceToken(held.token, "")
		m.attached = append(m.attached, outcome)
	case paste.Working:
	default:
		panic("session: unknown paste state")
	}
}

func (m *Model) textOrChip(text string) string {
	chars := len([]rune(text))
	if chars < textChipThreshold {
		return text
	}
	token := textToken(chars)
	m.chips = append(m.chips, Chip{Kind: TextChip, Chars: chars, Token: token, Text: text})
	return token
}

func (m *Model) InsertPaste(text string) tea.Cmd {
	return m.Update(tea.PasteMsg{Content: m.textOrChip(text)})
}

func Expand(task string, chips []Chip) string {
	for _, chip := range chips {
		if chip.Kind == TextChip {
			task = strings.Replace(task, chip.Token, chip.Text, 1)
		}
	}
	return task
}

func (m *Model) replaceToken(token, replacement string) {
	m.composer.SetValue(strings.Replace(m.composer.Value(), token, replacement, 1))
}

func (c Chip) label() string {
	switch c.Kind {
	case ImageChip:
		return c.Format + " " + widget.Size(c.Bytes) + " " + c.Name
	case TextChip:
		return "text, " + strconv.Itoa(c.Chars) + " characters"
	}
	panic("session: unknown chip kind")
}

func (m *Model) chipLines(chips []Chip) []string {
	lines := make([]string, 0, len(chips))
	for index, chip := range chips {
		branch := treeBranch
		if index == len(chips)-1 {
			branch = treeLast
		}
		lines = append(lines, look.Faint(widget.Fit(branch+chip.label(), m.textWidth())))
	}
	return lines
}
