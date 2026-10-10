package feed

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/markdown"
	roster "tofu/internal/subagent"
)

const (
	cardCacheCap     = 2048
	cardMinWidth     = 22
	cardPadX         = 2
	cardPadY         = 1
	cardBorderRows   = 2
	cardBorderSides  = 2
	tabSpaces        = "    "
	detailIndent     = "  "
	headMinWidth     = 16
	headInset        = 12
	detailPreview    = 8
	elapsedPrecision = 100 * time.Millisecond
	fullDiffNote     = "Full diff in File edits."
	runsTitle        = "runs"
	unnamedAgent     = "(unnamed sub-agent)"
	ownsTitle        = "owns"
	reportTitle      = "report"
	overlapWord      = "  overlaps "
	requesting       = "Requesting "
	toWorkOn         = " to work on "
	unnamedSubAgent  = "a sub-agent"
	metaSeparator    = "  |  "
	waitingOn        = "waiting on "
	sentTo           = "to "
	doneWord         = "done"
	codeFence        = "```"
	foldedCode       = "..."
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

type heldHeight struct {
	width    int
	expanded bool
	event    Event
	height   int
}

type cardCache struct {
	cards    map[string]cachedCard
	order    []string
	heights  map[string]heldHeight
	prose    markdown.Renderer
	sheet    *sheet
	owners   []Agent
	owned    map[identity][]string
	railRows map[railRow]string
}

func sameEvent(a, b Event) bool {
	return a.ID == b.ID && a.Actor == b.Actor && a.Target == b.Target && a.Instance == b.Instance &&
		a.Kind == b.Kind && a.State == b.State && a.Title == b.Title && a.Body == b.Body &&
		a.Path == b.Path && a.Op == b.Op && a.Added == b.Added && a.Removed == b.Removed &&
		a.At.Equal(b.At) && a.Elapsed == b.Elapsed && slices.Equal(a.Detail, b.Detail)
}

type draft struct {
	event *Event
	lines func(*cardCache, Event, cardKey) []string
}

func (m Model) aboutCards() (about []Event, lead int) {
	agent, picked := m.filteredAgent()
	if !picked {
		return nil, 0
	}
	if agent.Model != "" {
		about = append(about, m.aboutFilter(runsTitle, cmp.Or(agent.Definition, unnamedAgent)+" on "+agent.Model, nil))
	}
	if len(agent.Owns) > 0 {
		about = append(about, m.aboutFilter(ownsTitle, "", m.ownership(agent.Owns)))
	}
	if agent.Report != "" {
		return append(about, m.aboutFilter(reportTitle, agent.Report, nil)), len(about)
	}
	return about, len(about)
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

func sameOwners(a, b Agent) bool {
	return a.Name == b.Name && a.Instance == b.Instance && slices.Equal(a.Owns, b.Owns)
}

func (m Model) ownership(owns []string) []string {
	c := m.cards
	if !slices.EqualFunc(c.owners, m.agents, sameOwners) {
		c.owned, c.owners = map[identity][]string{}, make([]Agent, len(m.agents))
		for index, a := range m.agents {
			c.owners[index] = Agent{Name: a.Name, Instance: a.Instance, Owns: slices.Clone(a.Owns)}
		}
	}
	if lines, ok := c.owned[m.filter]; ok {
		return lines
	}
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
	c.owned[m.filter] = lines
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

func textWidth(width int) int {
	return max(cardMinWidth, width-2) - 2*cardPadX - cardBorderSides
}

func (m Model) sized(width int, d draft) (height int, wrapped bool) {
	e, c := *d.event, m.cards
	if held, ok := c.heights[e.ID]; ok && held.width == width && held.expanded == m.expanded[e.ID] && sameEvent(held.event, e) {
		return held.height, false
	}
	key, limit := m.cardKey(width, d.event), textWidth(width)
	height = cardBorderRows + 2*cardPadY
	for line := range strings.SplitSeq(strings.ReplaceAll(strings.Join(d.lines(c, e, key), "\n"), "\t", tabSpaces), "\n") {
		if lipgloss.Width(line) <= limit {
			height++
			continue
		}
		height += lipgloss.Height(lipgloss.Wrap(line, limit, ""))
	}
	c.hold(heldHeight{width, key.expanded, e, height})
	return height, true
}

func (c *cardCache) hold(held heldHeight) {
	if c.heights == nil {
		c.heights = make(map[string]heldHeight)
	}
	held.event.Detail = slices.Clone(held.event.Detail)
	c.heights[held.event.ID] = held
}

func (m Model) card(width int, d draft) card {
	e, key, c := *d.event, m.cardKey(width, d.event), m.cards
	if cached, ok := c.cards[e.ID]; ok && cached.key == key && sameEvent(cached.event, e) {
		return card{e.ID, cached.view, cached.height}
	}
	view := framed(d.lines(c, e, key), key)
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
	c.hold(heldHeight{width, key.expanded, e, height})
	return card{e.ID, view, height}
}

func role(e Event) (string, look.Color) {
	switch e.Kind {
	case KindTool:
		return "tool", look.Amber
	case KindFailure:
		return "failure", look.Red
	case KindThinking:
		return e.Kind.String(), look.FaintColor
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

func (c *cardCache) cardLines(e Event, key cardKey) []string {
	word, colour := role(e)
	_, said, talking := talk(e)
	if talking {
		word, colour = KindMessage.String(), look.Blue
	}
	left := look.AgentRef(actor(e).label()) + "  " + look.Style(colour).Render(word)
	right := look.Faint(key.age+"  ") + look.TypedID(e.Kind.String(), e.ID)
	headWidth := max(headMinWidth, key.width-headInset)
	head := look.Sides(left, right, headWidth)
	if lipgloss.Width(left)+lipgloss.Width(right)+1 > headWidth {
		head = left + "\n" + right
	}
	lines := []string{head}
	if talking {
		for _, line := range said {
			lines = append(lines, look.Title(line))
		}
		return append(lines, metaLine(e))
	}
	switch e.Kind {
	case KindThinking:
		for _, line := range strings.Split(proseOnly(strings.TrimSpace(e.Body)), "\n") {
			lines = append(lines, look.Style(look.FaintColor).Italic(true).Render(line))
		}
		return lines
	case KindSpawn:
		asked := look.Title(unnamedSubAgent)
		if e.Target != "" {
			asked = look.AgentRef(e.Target)
		}
		lines = append(lines, look.Title(requesting)+asked+look.Title(toWorkOn+e.Title))
		lines = append(lines, c.prose.Lines(e.Body, textWidth(key.width))...)
	case KindEdit:
		lines = append(lines, look.Title(e.Title), look.Muted(strings.TrimSpace(fullDiffNote+" "+e.Body)))
	default:
		lines = append(lines, look.Title(e.Title))
		switch {
		case e.Title == reportTitle:
			lines = append(lines, c.prose.Lines(e.Body, textWidth(key.width))...)
		case e.Body != "":
			lines = append(lines, look.Muted(e.Body))
		}
	}
	lines = append(lines, metaLine(e))
	shown := e.Detail
	if !key.expanded && len(shown) > detailPreview {
		shown = shown[:detailPreview]
	}
	for _, line := range shown {
		lines = append(lines, detailIndent+look.OutputLine(line))
	}
	if hidden := len(e.Detail) - len(shown); hidden > 0 {
		lines = append(lines, look.Faint(fmt.Sprintf("  … %d more lines · enter to expand", hidden)))
	}
	return lines
}

func proseOnly(thought string) string {
	var kept []string
	fenced := false
	for _, line := range strings.Split(thought, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), codeFence) {
			if !fenced {
				kept = append(kept, foldedCode)
			}
			fenced = !fenced
			continue
		}
		if !fenced {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func agentCardLines(_ *cardCache, e Event, _ cardKey) []string {
	lines := append([]string{look.AgentRef(actor(e).label()) + "  " + look.Style(look.Blue).Render(e.Title)}, e.Detail...)
	if e.Body != "" {
		lines = append(lines, look.Style(look.Text).Render(e.Body))
	}
	return lines
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
	switch {
	case e.Kind == KindSpawn && e.State == StateComplete:
		meta = look.Faint(doneWord)
	case e.Kind == KindSpawn && e.State == StateRunning && e.Target != "":
		meta += look.Faint(metaSeparator+waitingOn) + look.AgentRef(e.Target)
	case e.Kind != KindSpawn && e.Target != "":
		meta += look.Faint(metaSeparator+sentTo) + look.AgentRef(e.Target)
	}
	if e.Elapsed > 0 {
		meta += look.Faint(metaSeparator + e.Elapsed.Round(elapsedPrecision).String())
	}
	return meta
}
