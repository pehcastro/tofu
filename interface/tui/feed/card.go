package feed

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	roster "tofu/internal/subagent"
)

const (
	cardCacheCap     = 2048
	cardMinWidth     = 22
	cardPadX         = 2
	cardPadY         = 1
	cardBorderRows   = 2
	cardFixedRows    = 3
	headMinWidth     = 16
	headInset        = 12
	detailPreview    = 8
	elapsedPrecision = 100 * time.Millisecond
	fullDiffNote     = "Full diff in File edits."
	ownsTitle        = "owns"
	reportTitle      = "report"
	overlapWord      = "  overlaps "
)

type card struct {
	id, view string
	height   int
}

type cardKey struct {
	width              int
	selected, expanded bool
	age                string
}

type cachedCard struct {
	key    cardKey
	event  Event
	view   string
	height int
}

type cardCache struct {
	cards map[string]cachedCard
	order []string
}

func sameEvent(a, b Event) bool {
	return a.ID == b.ID && a.Actor == b.Actor && a.Target == b.Target && a.Instance == b.Instance &&
		a.Kind == b.Kind && a.State == b.State && a.Title == b.Title && a.Body == b.Body &&
		a.Path == b.Path && a.Op == b.Op && a.Added == b.Added && a.Removed == b.Removed &&
		a.At.Equal(b.At) && a.Elapsed == b.Elapsed && slices.Equal(a.Detail, b.Detail)
}

type draft struct {
	event  *Event
	render func(Event, cardKey) string
}

func (m Model) drafts() []draft {
	events := m.visible()
	drafts := make([]draft, 0, len(events)+2)
	agent, picked := m.filteredAgent()
	if picked && len(agent.Owns) > 0 {
		owns := m.aboutFilter(ownsTitle, "", m.ownership(agent.Owns))
		drafts = append(drafts, draft{&owns, renderAgentCard})
	}
	for index := range events {
		drafts = append(drafts, draft{&events[index], renderCard})
	}
	if picked && agent.Report != "" {
		report := m.aboutFilter(reportTitle, agent.Report, nil)
		drafts = append(drafts, draft{&report, renderAgentCard})
	}
	return drafts
}

func (m Model) filteredAgent() (Agent, bool) {
	at := slices.IndexFunc(m.agents, func(a Agent) bool { return identity{a.Name, a.Instance} == m.filter })
	if at < 0 {
		return Agent{}, false
	}
	return m.agents[at], true
}

func (m Model) aboutFilter(title, body string, detail []string) Event {
	return Event{ID: m.filter.label() + " " + title, Actor: m.filter.name, Instance: m.filter.instance, Title: title, Body: body, Detail: detail}
}

func (m Model) ownership(owns []string) []string {
	lines := make([]string, 0, len(owns))
	for _, glob := range owns {
		var holders []string
		for _, other := range m.agents {
			held := identity{other.Name, other.Instance}
			if held != m.filter && slices.ContainsFunc(other.Owns, func(theirs string) bool { return overlapping(glob, theirs) }) {
				holders = append(holders, held.label())
			}
		}
		if len(holders) == 0 {
			lines = append(lines, look.Muted(glob))
			continue
		}
		lines = append(lines, look.Style(look.Amber).Render(glob+overlapWord+strings.Join(holders, " ")))
	}
	return lines
}

func overlapping(one, other string) bool {
	var held roster.Roster
	var collision roster.CollisionError
	return held.Hold(roster.SubAgent{ID: one, Owns: []string{one}}) == nil && errors.As(held.Hold(roster.SubAgent{ID: other, Owns: []string{other}}), &collision)
}

func (m Model) cardKey(width int, e *Event) cardKey {
	return cardKey{width, !m.railFocused && m.selected == e.ID, m.expanded[e.ID], look.Age(m.now().Sub(e.At))}
}

func (m Model) sized(width int, d draft) card {
	expanded := m.expanded[d.event.ID]
	if cached, ok := m.cards.cards[d.event.ID]; ok && cached.key.width == width && cached.key.expanded == expanded && sameEvent(cached.event, *d.event) {
		return card{id: d.event.ID, height: cached.height}
	}
	shown := min(len(d.event.Detail), detailPreview+1)
	if expanded {
		shown = len(d.event.Detail)
	}
	room := max(cardMinWidth, width-2) - 2*cardPadX
	prose := (len(d.event.Body) + room - 1) / room
	return card{id: d.event.ID, height: cardBorderRows + 2*cardPadY + cardFixedRows + shown + prose}
}

func (m Model) card(width int, d draft) card {
	e, key, c := *d.event, m.cardKey(width, d.event), m.cards
	if cached, ok := c.cards[e.ID]; ok && cached.key == key && sameEvent(cached.event, e) {
		return card{e.ID, cached.view, cached.height}
	}
	view := d.render(e, key)
	if c.cards == nil {
		c.cards = make(map[string]cachedCard)
	}
	if _, exists := c.cards[e.ID]; !exists {
		if len(c.order) >= cardCacheCap {
			delete(c.cards, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, e.ID)
	}
	height := lipgloss.Height(view)
	e.Detail = slices.Clone(e.Detail)
	c.cards[e.ID] = cachedCard{key, e, view, height}
	return card{e.ID, view, height}
}

func role(e Event) (string, look.Color) {
	switch e.Kind {
	case KindTool:
		return "tool", look.Amber
	case KindFailure:
		return "failure", look.Red
	case KindEdit:
		switch e.Op {
		case OpAdded:
			return "new file", look.Mint
		case OpDeleted:
			return "deleted file", look.Red
		}
		return "file edit", look.Blue
	}
	return e.Kind.String(), look.Blue
}

func renderCard(e Event, key cardKey) string {
	word, colour := role(e)
	left := look.AgentRef(actor(e).label()) + "  " + look.Style(colour).Render(word)
	right := look.Faint(key.age+"  ") + look.TypedID(e.Kind.String(), e.ID)
	headWidth := max(headMinWidth, key.width-headInset)
	head := look.Sides(left, right, headWidth)
	if lipgloss.Width(left)+lipgloss.Width(right)+1 > headWidth {
		head = left + "\n" + right
	}
	lines := []string{head, look.Title(e.Title)}
	body := e.Body
	if e.Kind == KindEdit {
		body = strings.TrimSpace(fullDiffNote + " " + body)
	}
	if body != "" {
		lines = append(lines, look.Muted(body))
	}
	lines = append(lines, metaLine(e))
	shown := e.Detail
	if !key.expanded && len(shown) > detailPreview {
		shown = shown[:detailPreview]
	}
	for _, line := range shown {
		lines = append(lines, "  "+look.OutputLine(line))
	}
	if hidden := len(e.Detail) - len(shown); hidden > 0 {
		lines = append(lines, look.Faint(fmt.Sprintf("  … %d more lines · enter to expand", hidden)))
	}
	return framed(lines, key)
}

func renderAgentCard(e Event, key cardKey) string {
	lines := append([]string{look.AgentRef(actor(e).label()) + "  " + look.Style(look.Blue).Render(e.Title)}, e.Detail...)
	if e.Body != "" {
		lines = append(lines, look.Style(look.Text).Render(e.Body))
	}
	return framed(lines, key)
}

func framed(lines []string, key cardKey) string {
	border := look.FaintColor
	if key.selected {
		border = look.Mint
	}
	return lipgloss.NewStyle().Width(max(cardMinWidth, key.width-2)).Padding(cardPadY, cardPadX).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(string(border))).
		Render(strings.Join(lines, "\n"))
}

func metaLine(e Event) string {
	if e.Kind == KindEdit {
		return look.Style(look.Mint).Render(look.SignedLines(int64(e.Added))) + "  " + look.Style(look.Red).Render(look.SignedLines(-int64(e.Removed))) + look.Faint("  ·  @"+e.Path)
	}
	meta := look.Faint(e.State.String())
	if e.Target != "" {
		meta += look.Faint("  |  to ") + look.AgentRef(e.Target)
	}
	if e.Elapsed > 0 {
		meta += look.Faint("  |  " + e.Elapsed.Round(elapsedPrecision).String())
	}
	return meta
}
