package work

import (
	"strings"

	"tofu/interface/tui/theme"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const (
	minimumWidth = 20
	headMarker   = "⟩ "
	argsMarker   = "  args  "
	outMarker    = "  out   "
	gap          = "  "
	emptyTitle   = "nothing has run yet in this session"
	emptyBody    = "every tool call, its arguments and its output land here, whole, as soon as the turn makes them."
	pickHint     = "↑↓ pick   enter brings a reference into chat"
)

type Entry struct {
	ID      string
	Head    string
	Args    string
	Output  string
	Verdict string
	Failed  bool
	Bytes   int
}

func (e Entry) label() string { return strings.TrimSpace(e.Head) }

func (e Entry) Kind() string {
	kind, _, _ := strings.Cut(e.label(), " ")
	return kind
}

const referenceRunes = 200

func (e Entry) Reference() string {
	body := e.Output
	if body == "" {
		body = e.Args
	}
	runes := []rune(body)
	if len(runes) > referenceRunes {
		body = string(runes[:referenceRunes]) + "…"
	}
	return strings.TrimSpace(e.label() + ": " + body)
}

type Model struct {
	Entries   []Entry
	pick      int
	width     int
	height    int
	top       int
	following bool
}

func New() Model { return Model{following: true} }

func (m *Model) SetSize(width, height int) {
	m.width, m.height = max(width, minimumWidth), height
}

func (m *Model) Key(key string) {
	switch key {
	case "down", "j":
		m.Move(1)
	case "up", "k":
		m.Move(-1)
	case "pgup":
		m.scroll(-max(m.height-1, 1))
	case "pgdown":
		m.scroll(max(m.height-1, 1))
	case "home":
		m.following, m.top = false, 0
	case "end":
		m.following = true
	}
}

func (m *Model) scroll(by int) {
	m.following = false
	m.top = max(m.top+by, 0)
}

func (m *Model) Move(by int) {
	if next := m.pick + by; next >= 0 && next < len(m.Entries) {
		m.pick = next
	}
}

func (m Model) Picked() (Entry, bool) {
	if len(m.Entries) == 0 {
		return Entry{}, false
	}
	return m.Entries[m.pick], true
}

func (m *Model) JumpTo(typed string) bool {
	for index, entry := range m.Entries {
		if strings.HasPrefix(entry.ID, typed) || strings.HasSuffix(entry.ID, typed) {
			m.pick = index
			return true
		}
	}
	return false
}

func (m *Model) Append(entry Entry) {
	m.Entries = append(m.Entries, entry)
}

func (m *Model) Finish(id, output string, bytes int, failed bool) {
	for index := len(m.Entries) - 1; index >= 0; index-- {
		if m.Entries[index].ID == id {
			m.Entries[index].Output, m.Entries[index].Bytes, m.Entries[index].Failed = output, bytes, failed
			return
		}
	}
}

func (m *Model) Decide(tool, verdict string) {
	for index := len(m.Entries) - 1; index >= 0; index-- {
		if strings.HasPrefix(m.Entries[index].Head, tool) && m.Entries[index].Verdict == "" {
			m.Entries[index].Verdict = verdict
			return
		}
	}
}

func (m *Model) View() string {
	if len(m.Entries) == 0 {
		return strings.Join(pad(m.empty(), m.height), "\n")
	}
	var lines []string
	for index, entry := range m.Entries {
		lines = append(lines, m.block(index, entry)...)
	}
	visible := m.window(lines)
	visible = append(visible, theme.Faint().Render(widget.Fit(pickHint, m.width)))
	return strings.Join(pad(visible, m.height), "\n")
}

func (m Model) empty() []string {
	lines := []string{theme.Dim().Render(widget.Fit(emptyTitle, m.width)), ""}
	for _, line := range widget.Wrap(emptyBody, m.width) {
		lines = append(lines, theme.Faint().Render(line))
	}
	return lines
}

func (m Model) block(index int, entry Entry) []string {
	style := theme.Text()
	if index == m.pick {
		style = theme.Accent()
	}
	head := headMarker + entry.label()
	if entry.Verdict != "" {
		head += gap + entry.Verdict
	}
	if entry.Bytes > 0 {
		head += gap + widget.Size(entry.Bytes)
	}
	if id := trace.Short(entry.ID); id != "" {
		head += gap + id
	}
	lines := []string{style.Render(widget.Fit(head, m.width))}
	if entry.Args != "" {
		for _, line := range widget.Wrap(entry.Args, max(m.width-widget.Cells(argsMarker), 1)) {
			lines = append(lines, theme.Faint().Render(argsMarker+line))
		}
	}
	outStyle := theme.Tool()
	if entry.Failed {
		outStyle = theme.Fail()
	}
	if entry.Output != "" {
		for _, line := range widget.Wrap(entry.Output, max(m.width-widget.Cells(outMarker), 1)) {
			lines = append(lines, outStyle.Render(outMarker+line))
		}
	}
	return append(lines, "")
}

func (m *Model) window(lines []string) []string {
	room := max(m.height-1, 1)
	maxTop := max(len(lines)-room, 0)
	if m.following {
		m.top = maxTop
	}
	m.top = min(max(m.top, 0), maxTop)
	return lines[m.top:min(m.top+room, len(lines))]
}

func pad(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}
