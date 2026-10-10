package feed

import (
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/look"
	"tofu/internal/golden"
	roster "tofu/internal/subagent"
)

var sessionStart = time.Date(2026, 9, 25, 17, 29, 0, 0, time.UTC)

func sessionEvents() []Event {
	events := []Event{
		{ID: "w88", Actor: "research", Kind: KindTool, State: StateComplete, Title: "Read · internal/turn/loop.go", Body: "Mapped how a turn hands tool results back to the model.", Elapsed: 1200 * time.Millisecond},
		{ID: "w91", Actor: "orchestrator", Target: "go-dev 13", Kind: KindMessage, State: StateComplete, Title: "Sent task to go-dev", Body: "Build the feed package and keep input ownership local to each view."},
		{ID: "w92", Actor: "go-dev", Instance: 13, Kind: KindTool, State: StateComplete, Title: "Bash · go build ./interface/tui/feed/...", Body: "Built the package.", Detail: []string{"compiled 4 files", "go vet: clean"}, Elapsed: 3400 * time.Millisecond},
		{ID: "e47", Actor: "go-dev", Instance: 13, Kind: KindEdit, State: StateComplete, Title: "Update · interface/tui/feed/feed.go", Body: "Added the rail groups.", Path: "interface/tui/feed/feed.go", Added: 34, Removed: 12},
		{ID: "w95", Actor: "orchestrator", Target: "ui-audit 3", Kind: KindMessage, State: StateComplete, Title: "Sent review request", Body: "Check keyboard focus, mouse hit areas and narrow widths."},
		{ID: "w97", Actor: "ui-audit", Instance: 3, Kind: KindFailure, State: StateFailed, Title: "Narrow layout clipped a card", Body: "The feed lost its last row below 86 columns.", Detail: []string{"layout/width_80 failed", "layout/width_120 passed"}},
		{ID: "e48", Actor: "ui-audit", Instance: 3, Kind: KindEdit, State: StateComplete, Title: "Create · interface/tui/feed/feed_test.go", Path: "interface/tui/feed/feed_test.go", Op: OpAdded, Added: 31},
		{ID: "w98", Actor: "orchestrator", Target: "research", Kind: KindMessage, State: StateComplete, Title: "Asked research to compare picker patterns", Body: "Keep keyboard focus visible without filling every row."},
		{ID: "w99", Actor: "research", Kind: KindTool, State: StateComplete, Title: "Read · charm.land/bubbles list", Body: "Compared delegated filtering with a viewport.", Detail: []string{"list delegate owns item presentation", "viewport owns bounded content"}, Elapsed: 2400 * time.Millisecond},
		{ID: "e52", Actor: "go-dev", Instance: 13, Kind: KindEdit, State: StateComplete, Title: "Update · interface/tui/feed/card.go", Body: "Bounded event cards with typed ids.", Path: "interface/tui/feed/card.go", Added: 26, Removed: 8},
		{ID: "d11", Actor: "orchestrator", Kind: KindDecision, State: StateComplete, Title: "Gate allowed go test", Body: "The command reads only the package it names."},
		{ID: "w101", Actor: "go-dev", Instance: 13, Kind: KindTool, State: StateComplete, Title: "Bash · go test ./interface/tui/feed/...", Body: "Ran the cache equality tests.", Detail: []string{"28 passed · 0 failed", "retry once on a flaky clock", "go vet: clean"}, Elapsed: 3800 * time.Millisecond},
		{ID: "e53", Actor: "ui-audit", Instance: 3, Kind: KindEdit, State: StateComplete, Title: "Update · interface/tui/feed/input.go", Body: "Clicks read rendered rows.", Path: "interface/tui/feed/input.go", Added: 19, Removed: 11},
		{ID: "w102", Actor: "orchestrator", Target: "ui-audit 3", Kind: KindMessage, State: StateComplete, Title: "Sent final review", Body: "Check selection, colours and small widths."},
		{ID: "n4", Actor: "ui-audit", Instance: 3, Kind: KindNote, State: StateComplete, Title: "Design pass returned", Body: "Keep the quiet base and make metadata subordinate."},
		{ID: "e54", Actor: "go-dev", Instance: 14, Kind: KindEdit, State: StateComplete, Title: "Create · interface/tui/feed/rail.go", Path: "interface/tui/feed/rail.go", Op: OpAdded, Added: 88},
		{ID: "e55", Actor: "go-dev", Instance: 14, Kind: KindEdit, State: StateComplete, Title: "Delete · interface/tui/work/work.go", Path: "interface/tui/work/work.go", Op: OpDeleted, Removed: 412},
		{ID: "r7", Actor: "orchestrator", Kind: KindRequest, State: StateComplete, Title: "Model request", Body: "claude-sub/claude-opus-5 answered in one pass.", Elapsed: 9 * time.Second},
		{ID: "w104", Actor: "orchestrator", Target: "research", Kind: KindMessage, State: StateComplete, Title: "Asked research for search layouts", Body: "Compare a compact source list with a table."},
		{ID: "w105", Actor: "research", Kind: KindTool, State: StateComplete, Title: "Read · .local/sources", Body: "Every cloned harness keeps one stream.", Detail: []string{"opencode: one stream", "codex: one stream", "claude: one stream", "aider: one stream", "goose: one stream", "crush: one stream", "plandex: one stream", "cline: one stream", "roo: one stream", "zed: one stream"}, Elapsed: 5100 * time.Millisecond},
		{ID: "w106", Actor: "go-dev", Instance: 14, Kind: KindTool, State: StateFailed, Title: "Bash · go test ./interface/tui/work/...", Body: "The package no longer exists.", Detail: []string{"panic: no Go files"}, Elapsed: 700 * time.Millisecond},
		{ID: "w107", Actor: "orchestrator", Target: "go-dev 14", Kind: KindMessage, State: StateComplete, Title: "Parked go-dev 14", Body: "Wait for the app to stop importing work."},
		{ID: "e56", Actor: "go-dev", Instance: 13, Kind: KindEdit, State: StateComplete, Title: "Update · interface/tui/feed/input.go", Body: "Wheel scrolls without moving the selection.", Path: "interface/tui/feed/input.go", Added: 7, Removed: 3},
		{ID: "w108", Actor: "go-dev", Instance: 13, Kind: KindTool, State: StateComplete, Title: "Bash · go vet ./interface/tui/feed/...", Body: "Vet is clean.", Elapsed: 1100 * time.Millisecond},
		{ID: "w109", Actor: "orchestrator", Target: "ui-audit 3", Kind: KindMessage, State: StateComplete, Title: "Requested focus review", Body: "Check overlays, mouse hit areas and narrow widths."},
		{ID: "e57", Actor: "go-dev", Instance: 13, Kind: KindEdit, State: StateComplete, Title: "Update · interface/tui/feed/card.go", Body: "Detail lines use shell colours.", Path: "interface/tui/feed/card.go", Added: 21, Removed: 9},
		{ID: "w110", Actor: "ui-audit", Instance: 3, Kind: KindTool, State: StateRunning, Title: "Run layout checks", Body: "Testing 60x20, 80x24 and 120x36."},
	}
	for i := range events {
		events[i].At = sessionStart.Add(time.Duration(i) * 40 * time.Second)
	}
	return events
}

func sessionAgents() []Agent {
	return []Agent{
		{Name: "go-dev", Instance: 13, State: roster.Working, Doing: "Writing card.go", Since: 4 * time.Minute},
		{Name: "ui-audit", Instance: 3, State: roster.InReview, Doing: "Waiting on review", Since: 2 * time.Minute},
		{Name: "research", State: roster.Finished, Doing: "References mapped", Since: 11 * time.Minute},
		{Name: "go-dev", Instance: 14, State: roster.Parked, Doing: "Parked by orchestrator", Since: time.Minute},
	}
}

func sessionModel(clock *time.Time, agents int) Model {
	m := New(func() time.Time { return *clock })
	m.SetSize(120, 36)
	m.SetEvents(sessionEvents())
	m.SetAgents(true, sessionAgents()[:agents])
	return m
}

func freshCaches(m Model) Model {
	m.cards, m.rail, m.main = &cardCache{}, &look.PaneCache{}, &look.PaneCache{}
	return m
}

func TestViewGolden(t *testing.T) {
	clock := sessionStart.Add(19 * time.Minute)
	m := sessionModel(&clock, 3)
	m.SetAgents(true, append(sessionAgents()[:3], Agent{Name: "lint", State: roster.Errored, Doing: "Exited with status 1", Since: 6 * time.Minute}))
	golden.Assert(t, "sub-agents-120x36.golden", m.View())
}

func TestFeedHeaderNamesWhereThePageSits(t *testing.T) {
	clock := sessionStart.Add(19 * time.Minute)
	m := sessionModel(&clock, 3)
	header := func() string { return strings.SplitN(m.View(), "\n", 3)[1] }
	if got := header(); !strings.Contains(got, "/27 · wheel ↑ older") {
		t.Fatalf("at the newest with older cards above, the header says %q", got)
	}
	m.scroll = 1 << 20
	if got := header(); !strings.Contains(got, "1-4/27 · oldest") {
		t.Fatalf("with card 1 on screen and newer below, the header says %q", got)
	}
	m.SetEvents(sessionEvents()[:1])
	if got := header(); !strings.Contains(got, "1-1/1 · newest") {
		t.Fatalf("with every card on screen, the header says %q", got)
	}
}

func TestActivityCardCacheInvalidatesOnVisibleChanges(t *testing.T) {
	clock := sessionStart.Add(19 * time.Minute)
	m := sessionModel(&clock, 4)
	check := func(stage string) {
		t.Helper()
		got := m.View()
		if want := freshCaches(m).View(); got != want {
			t.Fatalf("%s: cached activity differs from uncached render", stage)
		}
	}
	check("initial")
	m.scroll = 13
	check("scroll")
	clock = clock.Add(3 * time.Hour)
	check("time label")
	m.Focus("w92")
	check("selection")
	m.Key("enter")
	check("expanded")
	m.SetFocus(true)
	check("focus")
	m.SetSize(80, 36)
	check("width")
	events := sessionEvents()
	events[26].Title, events[26].Detail = "Changed tool title", []string{"Changed tool detail"}
	m.SetEvents(events)
	check("event update")
	m.SetEvents(events[:20])
	check("events removed")
}

func TestFeedSidebarCacheMatchesUncached(t *testing.T) {
	clock := sessionStart.Add(19 * time.Minute)
	m := sessionModel(&clock, 4)
	check := func(stage string) {
		t.Helper()
		got := m.View()
		if want := freshCaches(m).View(); got != want {
			t.Fatalf("%s: cached feed sidebar differs from uncached render", stage)
		}
	}
	check("initial")
	m.scroll = 10
	check("scroll")
	m.Key("down")
	check("filter")
	m.SetFocus(false)
	check("focus")
	m.SetFrame(3)
	check("spinner")
	m.SetSize(80, 24)
	check("resize")
}

func BenchmarkProgressedScreenRender(b *testing.B) {
	b.Run("sub-agents", func(b *testing.B) {
		clock := sessionStart.Add(19 * time.Minute)
		m := sessionModel(&clock, 4)
		m.SetFrame(100)
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			m.scroll = i % 30
			_ = m.View()
		}
	})
}

func BenchmarkSteadyScreenRender(b *testing.B) {
	b.Run("sub-agents", func(b *testing.B) {
		clock := sessionStart.Add(19 * time.Minute)
		m := sessionModel(&clock, 4)
		m.SetFrame(100)
		m.scroll = 15
		_ = m.View()
		b.ReportAllocs()
		for b.Loop() {
			_ = m.View()
		}
	})
}

func BenchmarkWheelEventFrame(b *testing.B) {
	b.Run("sub-agents", func(b *testing.B) {
		clock := sessionStart.Add(19 * time.Minute)
		m := sessionModel(&clock, 4)
		m.SetFrame(100)
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			m.scroll = i % 30
			m.Wheel(-1)
			_ = m.View()
		}
	})
}

func BenchmarkActivityPageContinuedSession(b *testing.B) {
	clock := sessionStart.Add(19 * time.Minute)
	m := sessionModel(&clock, 4)
	b.ReportAllocs()
	for b.Loop() {
		m.page()
	}
}
