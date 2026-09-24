package links_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/links"
	"tofu/interface/tui/quote"
)

const (
	firstRow     = "alpha"
	secondRow    = "beta"
	pickerWidth  = 80
	pickerHeight = 12
)

type screen interface {
	Key(key string)
	View() string
	Anything() bool
}

type linksScreen struct{ links.Model }

func (s *linksScreen) Anything() bool { _, any := s.Picked(); return any }

type quoteScreen struct{ quote.Model }

func (s *quoteScreen) Anything() bool { _, any := s.Picked(); return any }

type picker struct {
	name string
	open func() screen
}

func bothPickers() []picker {
	return []picker{
		{"links", func() screen {
			var built linksScreen
			built.SetSize(pickerWidth, pickerHeight)
			built.Set([]links.Link{
				{URL: "https://go.dev/" + firstRow, From: "you", Count: 1},
				{URL: "https://go.dev/" + secondRow, From: "the answer", Count: 1},
			}, "")
			return &built
		}},
		{"quote", func() screen {
			var built quoteScreen
			built.SetSize(pickerWidth, pickerHeight)
			built.Set([]quote.Turn{
				{Event: "0f2c9b1a-1111-4aaa-8bbb-aaaaaade2233", From: "you", Text: firstRow + " was said first"},
				{Event: "0f2c9b1a-2222-4aaa-8bbb-bbbbbbc41099", From: "the agent", Text: secondRow + " was said second"},
			}, "")
			return &built
		}},
	}
}

func marked(t *testing.T, one screen) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(one.View()), "\n") {
		if strings.HasPrefix(line, "› ") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no row is marked as picked\n%s", ansi.Strip(one.View()))
	return ""
}

func typed(one screen, word string) {
	for _, letter := range strings.Split(word, "") {
		one.Key(letter)
	}
}

func TestBothPickersMoveThePickTheSameWayAndStopAtBothEnds(t *testing.T) {
	for _, each := range bothPickers() {
		t.Run(each.name, func(t *testing.T) {
			one := each.open()
			first := marked(t, one)
			if !strings.Contains(first, firstRow) {
				t.Fatalf("the picker opens on %q rather than the first row", first)
			}
			one.Key("down")
			if second := marked(t, one); !strings.Contains(second, secondRow) {
				t.Fatalf("down left the pick on %q", second)
			}
			for range 4 {
				one.Key("down")
			}
			if past := marked(t, one); !strings.Contains(past, secondRow) {
				t.Fatalf("down ran past the last row onto %q", past)
			}
			for range 4 {
				one.Key("up")
			}
			if back := marked(t, one); back != first {
				t.Fatalf("up ran past the first row onto %q, want %q", back, first)
			}
		})
	}
}

func TestBothPickersFilterAsYouTypeAndBackspaceTakesALetterBack(t *testing.T) {
	for _, each := range bothPickers() {
		t.Run(each.name, func(t *testing.T) {
			one := each.open()
			typed(one, secondRow)
			drawn := ansi.Strip(one.View())
			if !strings.Contains(drawn, "filter: "+secondRow) {
				t.Fatalf("the picker does not say what it is filtered by\n%s", drawn)
			}
			if strings.Contains(drawn, firstRow) {
				t.Fatalf("the filter kept a row that does not match\n%s", drawn)
			}
			for range len(secondRow) {
				one.Key("backspace")
			}
			whole := ansi.Strip(one.View())
			if strings.Contains(whole, "filter: ") {
				t.Fatalf("backspacing the filter away left the hint on screen\n%s", whole)
			}
			if !strings.Contains(whole, firstRow) || !strings.Contains(whole, secondRow) {
				t.Fatalf("backspacing the filter away did not bring both rows back\n%s", whole)
			}
		})
	}
}

func TestBothPickersSayWhenAFilterMatchesNothingAndPickNothing(t *testing.T) {
	for _, each := range bothPickers() {
		t.Run(each.name, func(t *testing.T) {
			one := each.open()
			typed(one, "zzz")
			if drawn := ansi.Strip(one.View()); !strings.Contains(drawn, "nothing here matches zzz") {
				t.Fatalf("a filter matching nothing draws no reason\n%s", drawn)
			}
			if one.Anything() {
				t.Fatal("a filter matching nothing still picks a row")
			}
		})
	}
}
