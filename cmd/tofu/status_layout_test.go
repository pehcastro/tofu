package main

import (
	"strconv"
	"strings"
	"testing"

	"tofu/interface/tui/theme"
	"tofu/internal/golden"
	"tofu/internal/widget"
)

const (
	barRunes    = "▓░"
	narrowWidth = 80
	wideWidth   = 120
)

func renderedAt(width int) []string {
	return strings.Split(strings.TrimSuffix(statusText(statusFixture(), plain, fixtureMoment(), width), "\n"), "\n")
}

func headAt(lines []string, number string) int {
	for at, line := range lines {
		if strings.Contains(line, cardTop) && strings.Contains(line, number) {
			return at
		}
	}
	return -1
}

func barColumn(line string) int {
	at := strings.IndexAny(line, barRunes)
	if at < 0 {
		return -1
	}
	return widget.Cells(line[:at])
}

func TestNoRenderedLineCollidesTheAccountIDWithTheState(t *testing.T) {
	for _, width := range []int{narrowWidth, wideWidth} {
		lines := renderedAt(width)
		head, state := "", ""
		for _, line := range lines {
			if strings.Contains(line, fixtureCodexAccount) {
				head = line
			}
			if strings.Contains(line, fixtureSpentState) {
				state = line
			}
		}
		if head == "" || state == "" {
			t.Fatalf("at %d columns the account or its state is missing:\n%s", width, strings.Join(lines, "\n"))
		}
		if head == state {
			t.Fatalf("at %d columns the account id and the state share a line: %q", width, head)
		}
		if !strings.HasSuffix(head, fixtureCodexAccount) {
			t.Fatalf("at %d columns something follows the account id: %q", width, head)
		}
		if fields := strings.Fields(state); len(fields) < 2 || fields[1] != factState {
			t.Fatalf("at %d columns the state carries no label: %q", width, state)
		}
	}
}

func TestNothingInTheListingTruncatesOrRunsPastTheEdge(t *testing.T) {
	for _, width := range []int{narrowWidth, wideWidth} {
		for _, line := range renderedAt(width) {
			if strings.Contains(line, "…") {
				t.Errorf("at %d columns a line truncates: %q", width, line)
			}
			if cells := widget.Cells(line); cells > width {
				t.Errorf("at %d columns a line is %d cells wide: %q", width, cells, line)
			}
		}
	}
}

func TestTwoAccountsUnderOneSubscriptionAreVisiblySeparated(t *testing.T) {
	for _, width := range []int{narrowWidth, wideWidth} {
		lines := renderedAt(width)
		first, second := headAt(lines, "#1"), headAt(lines, "#2")
		if first < 0 || second <= first {
			t.Fatalf("at %d columns the two accounts are not both drawn:\n%s", width, strings.Join(lines, "\n"))
		}
		between := lines[first+1 : second]
		closed, aired := false, false
		for _, line := range between {
			closed = closed || strings.HasPrefix(strings.TrimSpace(line), cardFoot)
			aired = aired || strings.TrimSpace(line) == ""
		}
		if !closed || !aired {
			t.Fatalf("at %d columns the first account is closed=%v and followed by air=%v:\n%s",
				width, closed, aired, strings.Join(between, "\n"))
		}
	}
}

func TestTheAccountTheTurnPassedOverSaysSoWithoutAskingForAttention(t *testing.T) {
	painted := statusText(statusFixture(), coloured, fixtureMoment(), narrowWidth)
	unchosen := cardOf(t, painted, "#2")
	if !strings.Contains(unchosen, statusUnchosen) {
		t.Fatalf("the account the picker passed over does not say so:\n%s", unchosen)
	}
	if strings.Contains(unchosen, theme.Warn().Render(statusUnchosen)) {
		t.Fatalf("an account with room that simply was not chosen is drawn as a warning:\n%s", unchosen)
	}
	if !strings.Contains(cardOf(t, painted, "#1"), statusInUse) {
		t.Fatalf("the chosen account does not say it is the one in use:\n%s", painted)
	}
}

func TestAnAccountThatNeedsNoAttentionCarriesNoAttentionColour(t *testing.T) {
	painted := statusText(statusFixture(), coloured, fixtureMoment(), narrowWidth)
	calm := cardOf(t, painted, "#4")
	if strings.Contains(calm, theme.Warn().Render(statusInUse)) {
		t.Fatalf("an account in use is drawn as a warning:\n%s", calm)
	}
	if warn, _, _ := strings.Cut(theme.Warn().Render("x"), "x"); warn != "" && strings.Contains(calm, warn) {
		t.Fatalf("an account that needs no attention carries the warning colour:\n%s", calm)
	}
	if loud := cardOf(t, painted, "#3"); !strings.Contains(loud, theme.Warn().Render(fixtureSpentState)) {
		t.Fatalf("the spent account is not drawn as a warning:\n%s", loud)
	}
}

func TestEveryWindowBarStartsAtTheSameColumn(t *testing.T) {
	for _, width := range []int{narrowWidth, wideWidth} {
		column, bars, sawWidestLabel := -1, 0, false
		for _, line := range renderedAt(width) {
			at := barColumn(line)
			if at < 0 {
				continue
			}
			bars++
			sawWidestLabel = sawWidestLabel || strings.Contains(line, fixtureWidestWindow)
			if column < 0 {
				column = at
			}
			if at != column {
				t.Fatalf("at %d columns a bar starts at %d and another at %d: %q", width, column, at, line)
			}
		}
		if bars != fixtureBars {
			t.Fatalf("at %d columns %d bars are drawn, want %d", width, bars, fixtureBars)
		}
		if !sawWidestLabel {
			t.Fatalf("at %d columns %s is not drawn", width, fixtureWidestWindow)
		}
	}
}

func TestTheListingGoldens(t *testing.T) {
	for _, width := range []int{narrowWidth, wideWidth} {
		golden.Assert(t, "status-"+strconv.Itoa(width)+".golden", statusText(statusFixture(), plain, fixtureMoment(), width))
	}
}
