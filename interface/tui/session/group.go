package session

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/progress"
	"tofu/internal/host"
	"tofu/internal/toolcard"
	"tofu/internal/widget"
)

const (
	callIndent   = "  "
	openIndent   = "   "
	actGap       = " · "
	boardIcon    = "▣ "
	gitIcon      = "◆ "
	runningWord  = "running"
	finishedWord = "finished"
	createdWord  = "created"
	pushedWord   = "pushed"
	collapseWord = "collapse"

	openedVerb    = "opened"
	movedVerb     = "moved"
	committedVerb = "committed"
	subjectEnd    = "  "
	moveStep      = " > "
)

type group struct {
	calls   []Entry
	agents  int
	running int
	live    bool
	headed  bool
	open    bool
}

func (e Entry) standsAlone() bool {
	return e.asked || e.Card != nil && e.Card.Family == host.CardFamily(toolcard.Asked)
}

func (e Entry) act() (string, string, bool) {
	card := e.Card
	if card == nil || e.Failed || card.Tone == host.CardTone(toolcard.Fail) || card.Object == "" {
		return "", "", false
	}
	switch {
	case card.Family == host.CardFamily(toolcard.Board) && card.Verb == openedVerb:
		return boardIcon, joined(card.Object, createdWord, card.Meta), true
	case card.Family == host.CardFamily(toolcard.Board) && card.Verb == movedVerb && card.From != "" && card.To != "":
		return boardIcon, joined(card.Object, movedVerb+" "+card.From+moveStep+card.To), true
	case card.Family == host.CardFamily(toolcard.Board) && card.Verb == movedVerb:
		return boardIcon, joined(card.Object, strings.TrimSpace(movedVerb+" "+card.Meta)), true
	case card.Family == host.CardFamily(toolcard.Git) && card.Verb == committedVerb:
		subject, _, _ := strings.Cut(card.Meta, subjectEnd)
		return gitIcon, joined(card.Object, subject), true
	case card.Family == host.CardFamily(toolcard.Git) && card.Verb == pushedWord:
		return gitIcon, joined(pushedWord, card.Object), true
	}
	return "", "", false
}

func joined(words ...string) string {
	var kept []string
	for _, word := range words {
		if word = strings.TrimSpace(word); word != "" {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, actGap)
}

func (m *Model) groupOf(start int) group {
	_, end := m.blockAt(start)
	g := group{calls: m.entries[start:end], running: -1, open: m.opened[m.entries[start].ID], live: m.Busy}
	for at, call := range g.calls {
		if call.running() {
			g.running = at
		}
		g.headed = g.headed || !call.standsAlone()
	}
	for _, later := range m.entries[end:] {
		g.live = g.live && !later.message()
	}
	for before := start - 1; before >= 0 && !m.entries[before].message(); before-- {
		g.agents += len(m.entries[before].SubAgents)
	}
	return g
}

func (m *Model) groupKey(key drawKey, start int) (drawKey, bool) {
	g := m.groupOf(start)
	key.calls, key.open, key.spawn = len(g.calls), g.open, strconv.Itoa(g.agents)
	for _, call := range g.calls {
		if call.standsAlone() {
			key.verdict += call.ID + call.verdictShown() + " "
		}
		if _, words, acted := call.act(); acted {
			key.verdict += words + " "
		}
	}
	return key, g.live
}

func (m *Model) groupLines(start int) []string {
	g := m.groupOf(start)
	room := m.textWidth()
	var lines []string
	if g.headed {
		right := ""
		if g.open {
			right = look.Faint(collapseWord)
		}
		lines = append(lines, continuation+look.Sides(m.workRow(g), right, room))
	}
	for _, call := range g.calls {
		icon, words, acted := call.act()
		switch {
		case call.standsAlone():
			lines = append(lines, continuation+m.toolLine(call))
		case acted:
			lines = append(lines, continuation+look.Style(look.ReferenceID).Render(icon)+look.Style(look.Text).Render(ansi.Truncate(words, max(room-widget.Cells(icon), 1), "…")))
		case g.open:
			lines = append(lines, continuation+openIndent+look.Muted(ansi.Truncate(cardWords(call), max(room-widget.Cells(openIndent), 1), "…")))
		}
	}
	return lines
}

func (m *Model) workRow(g group) string {
	counts := "(" + strconv.Itoa(len(g.calls)) + ") steps"
	switch {
	case g.agents == 1:
		counts += actGap + "1 agent"
	case g.agents > 1:
		counts += actGap + strconv.Itoa(g.agents) + " agents"
	}
	if g.live {
		return look.Muted(progress.Spin(m.frame) + " " + m.leadDoing(g) + actGap + counts)
	}
	var ended time.Time
	for _, call := range g.calls {
		if call.Ended.After(ended) {
			ended = call.Ended
		}
	}
	return look.Muted(strings.Join([]string{finishedWord, counts, widget.Until(ended.Sub(g.calls[0].Started))}, actGap))
}

func (m *Model) leadDoing(g group) string {
	if g.running < 0 {
		word, _ := m.phase.drawn()
		return word
	}
	call := g.calls[g.running]
	if call.Card == nil || call.Card.Family == host.CardFamily(toolcard.Ran) {
		return runningWord + " " + call.Head
	}
	return call.Card.Doing
}

func (m *Model) Toggle(id string) bool {
	for at, entry := range m.entries {
		if entry.ID != id || id == "" {
			continue
		}
		if start, _ := m.blockAt(at); !m.folds(at) || start != at || !m.groupOf(at).headed {
			return false
		}
		m.opened[id] = !m.opened[id]
		m.revision++
		return true
	}
	return false
}

func cardWords(call Entry) string {
	if call.Card == nil {
		return call.Head
	}
	return strings.TrimSpace(call.Card.Verb + " " + call.Card.Object)
}
