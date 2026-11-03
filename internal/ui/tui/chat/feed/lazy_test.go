package feed

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const lazyCopies = 10

func manyEvents() []Event {
	var events []Event
	for round := range lazyCopies {
		for _, e := range sessionEvents() {
			e.ID += "-" + strconv.Itoa(round)
			events = append(events, e)
		}
	}
	return events
}

func everyCardDrawn(m Model) string {
	var rows []string
	for index, d := range m.sheet().drafts {
		if index > 0 {
			rows = append(rows, make([]string, m.gap)...)
		}
		key := m.cardKey(m.feedWidth(), d.event)
		rows = append(rows, strings.Split(framed(d.lines(m.cards, *d.event, key), key), "\n")...)
	}
	top := fromTop(len(rows), m.pageHeight(), m.scroll)
	return strings.Join(rows[top:min(len(rows), top+m.pageHeight())], "\n")
}

func TestAPageDrawnFromTheVisibleCardsMatchesOneDrawnFromEveryCard(t *testing.T) {
	clock := sessionStart.Add(19 * time.Minute)
	m := sessionModel(&clock, 4)
	m.SetEvents(manyEvents())
	for _, size := range [][2]int{{120, 36}, {80, 24}, {120, 36}, {61, 30}, {140, 50}} {
		m.SetSize(size[0], size[1])
		for _, scroll := range []int{0, 13, 90, 400, 1_000_000} {
			m.scroll = scroll
			if got, want := m.page().view, everyCardDrawn(m); got != want {
				t.Fatalf("%dx%d scrolled %d: the visible cards draw\n%s\nevery card draws\n%s", size[0], size[1], scroll, got, want)
			}
		}
		m.scroll = 0
		m.Focus("w92-3")
		m.Key("enter")
		if got, want := m.page().view, everyCardDrawn(m); got != want {
			t.Fatalf("%dx%d revealing an expanded card: the visible cards draw\n%s\nevery card draws\n%s", size[0], size[1], got, want)
		}
		m.Key("enter")
	}
}
