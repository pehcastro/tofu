package session

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/paste"
	"tofu/internal/host"
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
	Kind  ChipKind
	Chars int
	Token string
	Text  string
}

type pendingPaste struct {
	index int
	token string
}

func pastingToken(index int) string { return pastingTokenHead + strconv.Itoa(index) + "]" }

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
	kept := strings.Contains(m.composer.Value(), held.token)
	switch outcome.State {
	case paste.Ready:
		if kept {
			token := host.ImageToken(outcome.Index)
			m.replaceToken(held.token, token)
			m.chips = append(m.chips, Chip{Kind: ImageChip, Token: token})
		}
	case paste.Textual:
		if kept {
			m.replaceToken(held.token, m.textOrChip(outcome.Text))
		}
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
	return strings.ReplaceAll(task, messageToken, quoteToken)
}

func (m *Model) replaceToken(token, replacement string) {
	text, at := m.composer.Value(), m.caret()
	start := strings.Index(text, token)
	if start < 0 {
		return
	}
	end := start + len(token)
	if at <= start {
		m.setDraft(text[:at], text[at:start]+replacement+text[end:])
		return
	}
	at = max(at, end)
	m.setDraft(text[:start]+replacement+text[end:at], text[at:])
}

func (m *Model) blocks() []string {
	tokens := make([]string, 0, len(m.chips)+len(m.pending))
	for _, chip := range m.chips {
		tokens = append(tokens, chip.Token)
	}
	for _, row := range m.pending {
		tokens = append(tokens, row.token)
	}
	return tokens
}

func spans(text string, tokens []string) [][2]int {
	var found [][2]int
	for _, token := range tokens {
		for from := 0; ; {
			at := strings.Index(text[from:], token)
			if at < 0 {
				break
			}
			from += at + len(token)
			found = append(found, [2]int{from - len(token), from})
		}
	}
	return found
}

func (m *Model) deleteWhole(before string) {
	after, at := m.composer.Value(), m.caret()
	cut := len(before) - len(after)
	if cut <= 0 || before[:at] != after[:at] || before[at+cut:] != after[at:] {
		return
	}
	low, high := at, at+cut
	for _, span := range spans(before, m.blocks()) {
		if span[0] < at+cut && span[1] > at {
			low, high = min(low, span[0]), max(high, span[1])
		}
	}
	if low != at || high != at+cut {
		m.setDraft(before[:low], before[high:])
	}
}

func (m *Model) stepOver(fromRow, fromCol int) {
	row, col := m.composer.Line(), m.composer.Column()
	line := strings.Split(m.composer.Value(), "\n")[row]
	at := len(string([]rune(line)[:col]))
	for _, span := range spans(line, m.blocks()) {
		if at <= span[0] || at >= span[1] {
			continue
		}
		edge := span[1]
		if row == fromRow && col < fromCol {
			edge = span[0]
		}
		m.composer.SetCursorColumn(utf8.RuneCountInString(line[:edge]))
		return
	}
}

func (m *Model) chipLines(chips []Chip) []string {
	var texts, lines []string
	images := 0
	for _, chip := range chips {
		switch chip.Kind {
		case ImageChip:
			images++
		case TextChip:
			texts = append(texts, "text, "+strconv.Itoa(chip.Chars)+" characters")
		default:
			panic("session: unknown chip kind")
		}
	}
	switch images {
	case 0:
	case 1:
		lines = append(lines, "Attached 1 Image")
	default:
		lines = append(lines, "Attached "+strconv.Itoa(images)+" Images")
	}
	for index, text := range texts {
		branch := treeBranch
		if index == len(texts)-1 {
			branch = treeLast
		}
		lines = append(lines, branch+text)
	}
	for index, line := range lines {
		lines[index] = look.Faint(widget.Fit(line, m.textWidth()))
	}
	return lines
}
