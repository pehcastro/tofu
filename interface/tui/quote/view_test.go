package quote

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
)

func picker(t *testing.T, turns []Turn, keys ...string) Model {
	t.Helper()
	var view Model
	view.SetSize(80, 12)
	view.Set(turns, "")
	for _, key := range keys {
		view.Key(key)
	}
	return view
}

func drawn(t *testing.T, turns []Turn, keys ...string) string {
	t.Helper()
	return ansi.Strip(picker(t, turns, keys...).View())
}

func TestTheIDAndSpeakerColumnsKeepTheirOwnWidths(t *testing.T) {
	golden.Assert(t, "quote-80x12.golden", picker(t, Collect(talked())).View())
}

func TestAnEmptySessionOpensThePickerAndSaysThereIsNothingToQuote(t *testing.T) {
	screen := drawn(t, nil)
	if !strings.Contains(screen, emptyTitle) {
		t.Fatalf("an empty session draws nothing that says so\n%s", screen)
	}
	if !strings.Contains(screen, "0 turns") {
		t.Errorf("the picker does not count what it holds\n%s", screen)
	}
}

func TestThePickerShowsTheIDTheSpeakerAndEnoughTextToTellTwoTurnsApart(t *testing.T) {
	screen := drawn(t, Collect(talked()))
	for _, want := range []string{"#c41099", "the agent", "the gate reads library/policy/shell.yaml", "ran read, glob", "you", "read the policy before the wire"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the picker does not draw %q\n%s", want, screen)
		}
	}
}

func TestTypingFiltersThePickerAndPickingFollowsTheFilteredRows(t *testing.T) {
	turns := Collect(talked())
	screen := drawn(t, turns, "g", "l", "o", "b")
	if !strings.Contains(screen, "ran read, glob") {
		t.Fatalf("the filter dropped the row that matches\n%s", screen)
	}
	if strings.Contains(screen, "read the policy before the wire") {
		t.Errorf("the filter kept a row that does not match\n%s", screen)
	}
	if !strings.Contains(screen, "1 turn") {
		t.Errorf("the filtered count is wrong\n%s", screen)
	}

	one, picked := picker(t, turns, "g", "l", "o", "b").Picked()
	if !picked || one.Text != "ran read, glob" {
		t.Fatalf("the pick after filtering is %#v", one)
	}
}

func TestAFilterThatMatchesNothingSaysSoRatherThanLookingEmpty(t *testing.T) {
	screen := drawn(t, Collect(talked()), "z", "z", "z")
	if !strings.Contains(screen, noMatch+"zzz") {
		t.Fatalf("a filter matching nothing draws no reason\n%s", screen)
	}
}

func TestTroubleReadingTheRecordIsDrawnInsteadOfAnEmptyList(t *testing.T) {
	var view Model
	view.SetSize(80, 12)
	view.Set(nil, "nothing is recorded under this repository yet")
	if screen := ansi.Strip(view.View()); !strings.Contains(screen, "nothing is recorded") {
		t.Fatalf("the picker hides why it is empty\n%s", screen)
	}
}
