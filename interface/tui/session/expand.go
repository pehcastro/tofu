package session

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
)

const FiredHead = "fired"

func (e Entry) expandable() bool { return e.Kind == Tool && e.ID != "" || e.Head == FiredHead }

func (m *Model) LastExpandable() (id string, call bool) {
	for index := len(m.entries) - 1; index >= 0; index-- {
		if m.folds(index) {
			start, _ := m.blockAt(index)
			return m.entries[start].ID, true
		}
		if entry := m.entries[index]; entry.expandable() {
			return entry.ID, entry.Kind == Tool
		}
	}
	return "", false
}

func (m *Model) Expansion(id string, width int) (string, []string, bool) {
	at := slices.IndexFunc(m.entries, func(entry Entry) bool { return entry.Kind == Tool && entry.ID == id })
	if at < 0 {
		return "", nil, false
	}
	entry := m.entries[at]
	head := look.Sides(look.Title(entry.Head), look.TypedID(toolKind, m.FeedID(entry.ID)), width)
	body := hardWrapped(cmp.Or(entry.Detail, entry.Body), width, look.Muted)
	if decision := entry.Decision; decision != nil {
		body = append(append(body, "", verdictStyle(decision.Verdict).Render(decision.Verdict.String())), decisionLines(*decision, width)...)
	}
	status, statusStyle := m.callStatus(entry)
	if entry.running() {
		status = "running " + status
	}
	body = append(body, "", statusStyle.Render(status))
	if entry.Output != "" {
		body = append(append(body, ""), hardWrapped(entry.Output, width, look.OutputLine)...)
	}
	return head, body, true
}

func hardWrapped(text string, width int, paint func(string) string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		for _, piece := range strings.Split(ansi.Hardwrap(line, width, true), "\n") {
			lines = append(lines, paint(piece))
		}
	}
	return lines
}

func (m *Model) CallBeside(id string, step int) string {
	var calls []string
	for _, entry := range m.entries {
		if entry.Kind == Tool && entry.ID != "" {
			calls = append(calls, entry.ID)
		}
	}
	at := slices.Index(calls, id)
	if at < 0 {
		at = len(calls)
	}
	if next := at + step; next >= 0 && next < len(calls) {
		return calls[next]
	}
	return id
}

func (m *Model) ExpandAt(x, row int) (id string, call bool) {
	rows := m.transcriptRows()
	if row < 0 || row >= rows {
		return "", false
	}
	_, _, from := m.scrollMetrics(rows)
	row += from.line
	for index := from.entry; index < len(m.entries); {
		start, end := m.blockAt(index)
		lines := m.blockLines(start)
		if row < len(lines) {
			if m.folds(start) && row >= m.lead(start) {
				return m.entries[start].ID, true
			}
			if last := m.entries[end-1]; last.expandable() && pointer.TextHit(ansi.Strip(lines[row]), expandLabel, x) {
				return last.ID, last.Kind == Tool
			}
			return "", false
		}
		row -= len(lines)
		index = end
	}
	return "", false
}
