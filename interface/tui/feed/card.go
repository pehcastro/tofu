package feed

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
)

const (
	cardCacheCap     = 2048
	cardMinWidth     = 22
	cardPadX         = 2
	cardPadY         = 1
	headMinWidth     = 16
	headInset        = 12
	detailPreview    = 8
	elapsedPrecision = 100 * time.Millisecond
	fullDiffNote     = "Full diff in File edits."
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

func (m Model) cardsFor(width int) []card {
	events := m.visible()
	cards := make([]card, len(events))
	for i, e := range events {
		cards[i] = m.card(width, e)
	}
	return cards
}

func (m Model) card(width int, e Event) card {
	key := cardKey{width, !m.railFocused && m.selected == e.ID, m.expanded[e.ID], look.Age(m.now().Sub(e.At))}
	c := m.cards
	if cached, ok := c.cards[e.ID]; ok && cached.key == key && sameEvent(cached.event, e) {
		return card{e.ID, cached.view, cached.height}
	}
	view := renderCard(e, key)
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
