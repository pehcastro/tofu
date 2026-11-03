package frame

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"tofu/internal/konst"
)

var (
	testStarted = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	testHead    = Head{Path: "bob", Branch: "develop", SessionName: "north-ridge-lantern", SessionID: "0199a4c27f3a9c1e", Started: testStarted, At: testStarted.Add(12*time.Minute + 30*time.Second)}
	testTabs    = []Tab{{Label: "chat"}, {Label: "sub-agents", Count: 3}, {Label: "file edits"}, {Label: "shells"}}
)

func TestSettingsCogIsTextAndOwnsOnlyItsCells(t *testing.T) {
	for _, width := range []int{60, 80, 120, 160} {
		row, hits := Top(testHead, testTabs, 0, width)
		line := ansi.Strip(row)
		at := strings.LastIndex(line, settingsLink)
		if at < 0 {
			t.Fatalf("%d-column top row lost the settings link: %q", width, line)
		}
		x := ansi.StringWidth(line[:at])
		link := hits[len(hits)-1]
		if link.Index != settingsIndex || link.Start != x || link.End != x+ansi.StringWidth(settingsLink) {
			t.Fatalf("%d-column settings hit %+v does not cover exactly the link at %d", width, link, x)
		}
	}
}

func TestNavigationTabsAcceptMouseClicks(t *testing.T) {
	row, hits := Top(testHead, testTabs, 0, 120)
	cells := []rune(ansi.Strip(row))
	for index, want := range []string{" chat ", " sub-agents (3) ", " file edits ", " shells "} {
		hit := hits[index]
		if got := string(cells[hit.Start:hit.End]); hit.Index != index || got != want {
			t.Errorf("hit %d %+v covers %q, want %q", index, hit, got, want)
		}
	}
}

func TestTabsAndTheRightSideNeverTouch(t *testing.T) {
	for _, width := range []int{60, 77, 80, 104, 105, 119, 120, 144, 145, 160} {
		row, hits := Top(testHead, testTabs, 1, width)
		line := []rune(ansi.Strip(row))
		if len(line) != width {
			t.Errorf("%d-column top row is %d cells: %q", width, len(line), string(line))
		}
		if len(hits) != len(testTabs)+1 {
			t.Fatalf("%d-column top row has %d hits, want every tab and the link: %q", width, len(hits), string(line))
		}
		tabsEnd := hits[len(testTabs)-1].End
		rest := string(line[tabsEnd:])
		if blank := len(rest) - len(strings.TrimLeft(rest, " ")); blank < sideGap {
			t.Errorf("%d-column tabs and right side are %d cells apart: %q", width, blank, string(line))
		}
	}
}

func twoSourcesInUse() Status {
	return Status{
		Context: Context{Used: 118000, Budget: konst.ContextCeilingTokens},
		Quotas: []Quota{
			{Label: "claude-sub 5h", Fraction: 0.62, Reported: true},
			{Label: "codex-sub 5h", Fraction: 0.4, Reported: true},
		},
		InUse: []string{"claude-sub", "codex-sub"},
		At:    testHead.At,
	}
}

func TestEverySourceInUseKeepsItsPercentageAtEveryWidth(t *testing.T) {
	right := ChatRight("claude-sub/claude-opus-5", "medium")
	for _, width := range []int{160, 150, 149, 140, 120, 110, 100, 90, 82, 81, 60} {
		line := ansi.Strip(Footer(twoSourcesInUse(), width, right))
		t.Logf("%3d |%s|", width, line)
		if got := ansi.StringWidth(line); got != width {
			t.Errorf("%d-column footer is %d cells: %q", width, got, line)
		}
		wanted := []string{"claude 62%", "codex 40%"}
		if width >= secondSourceColumns {
			wanted = []string{"claude-sub 5h 62%", "codex-sub 5h 40%"}
		}
		for _, want := range wanted {
			if !strings.Contains(line, want) {
				t.Errorf("%d-column footer dropped %q while a source it names is in use: %q", width, want, line)
			}
		}
		if !strings.Contains(line, "claude-opus-5") && strings.Contains(line, "reasoning") {
			t.Errorf("%d-column footer dropped the model and kept the reasoning: %q", width, line)
		}
	}
}

func TestOneSourceInUseKeepsItsFullLabelBelowTheSecondSourceWidth(t *testing.T) {
	status := twoSourcesInUse()
	status.InUse = []string{"claude-sub"}
	line := ansi.Strip(Footer(status, 140, ChatRight("claude-sub/claude-opus-5", "medium")))
	if !strings.Contains(line, "claude-sub 5h 62%") || strings.Contains(line, "codex") {
		t.Errorf("one source in use at 140 columns drew %q, want claude-sub 5h 62%% and no codex", line)
	}
}

func BenchmarkTopAndFooter(b *testing.B) {
	status := Status{
		Context: Context{Used: 118000, Budget: konst.ContextCeilingTokens},
		Quotas:  []Quota{{Label: "claude-sub #1", Fraction: 0.62, Reported: true, ResetsAt: testHead.At.Add(3 * time.Hour)}},
		InUse:   []string{"claude-sub"},
		At:      testHead.At,
	}
	right := ChatRight("claude-sub/claude-opus-5", "high")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = Top(testHead, testTabs, 0, 120)
		_ = Footer(status, 120, right)
	}
}
