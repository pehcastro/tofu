package tui

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/crew"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const testRelease = "test"

func newTestApp(options Options) *App {
	options.Release = testRelease
	return New(options)
}

func fixedClock() func() time.Time {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	return func() time.Time { return at.Add(138 * time.Second) }
}

func bothWires() []Wire {
	return []Wire{
		{Name: "codex", Model: "gpt-5.6-sol", Provider: "openai"},
		{Name: "anthropic", Model: "claude-opus-5", Provider: "anthropic"},
	}
}

func anthropicAlone() []Wire {
	return []Wire{{Name: "anthropic", Model: "claude-opus-5", Provider: "anthropic"}}
}

func sessionApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: bothWires})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	app.Update(frame.Quota{
		Label:    "codex 7d",
		Fraction: 0.62,
		Reported: true,
		ResetsAt: time.Date(2026, 9, 19, 18, 0, 0, 0, time.Local),
	})
	for _, event := range []Event{
		{Kind: EventContext, Context: frame.Context{Used: 118000, Budget: 250000}},
		{Kind: EventStats, Model: "gpt-5.6-sol-2026-09-01", TokensIn: 284000, TokensOut: 61000, Decisions: 3},
		{Kind: EventText, Text: "reading the gate first, then the policy that decides it."},
		{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/judge/policy/toolgate.go"},
		{Kind: EventDecision, Decision: allowed()},
		{Kind: EventToolResult, ID: "c1", Text: "412 lines, 11.8 KB"},
		{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: "go test ./internal/judge/..."},
		{Kind: EventDecision, Decision: asked()},
		{Kind: EventToolResult, ID: "c2", Text: "ok tofu/internal/judge 0.42s"},
		{Kind: EventDone, Text: "stopped after"},
	} {
		app.Update(event)
	}
	return app
}

func allowed() *session.Decision {
	return &session.Decision{
		Tool:    "read",
		Verdict: session.Allow,
		Answers: []session.Answer{
			{Question: "risk", Value: 0, Max: 3},
			{Question: "approval", Value: 0.04, Max: 1},
		},
		Reason: session.Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 0},
	}
}

func asked() *session.Decision {
	return &session.Decision{
		Tool:    "bash",
		Verdict: session.Ask,
		Answers: []session.Answer{
			{Question: "approval", Value: 0.75, Max: 1},
			{Question: "from_untrusted", Value: 0.02, Max: 1},
			{Question: "risk", Value: 2, Max: 3},
			{Question: "user_requested", Value: 0.11, Max: 1},
		},
		Reason: session.Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 2},
	}
}

const (
	longCommand = `cd /home/dev/silo; for d in internal/* interface/* cmd/* catalog/*; ` +
		`do n=$(find "$d" -name '*.go' | wc -l); echo "$d $n"; done`
	longIntent     = "for d in internal/* interface/* cmd/* +3 more"
	forkNoticeHead = "⟳ forking the session"
	oversizeRead   = "12.1 KB, first and last part kept"
	prose          = "counting the go files under each root, then reading the rules."
)

func liveApp(t *testing.T, at *time.Time) *App {
	t.Helper()
	app := phaseApp(t, at)
	app.Update(Event{Kind: EventText, Text: prose})
	app.Update(Event{Kind: EventToolCall, ID: "c0", Tool: "read", Text: "internal/turn/loop.go"})
	app.Update(Event{Kind: EventToolResult, ID: "c0", Text: "84 lines, 2.1 KB"})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "CLAUDE.md"})
	*at = at.Add(1200 * time.Millisecond)
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: oversizeRead})
	app.Update(Event{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: longIntent, Detail: longCommand})
	return app
}

func TestAnOversizeReadAndALongCommandEachTakeOneLine(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := liveApp(t, &at)
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	content := ansi.Strip(app.View().Content)
	for _, want := range []string{"⟩ read CLAUDE.md", oversizeRead, "⟩ bash " + longIntent} {
		if !strings.Contains(content, want) {
			t.Errorf("the session does not show %q\n%s", want, content)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if cells := ansi.StringWidth(line); cells > 80 {
			t.Errorf("a row is %d cells wide\n%s", cells, line)
		}
	}
	rows := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "⟩ read CLAUDE.md") || strings.Contains(line, "⟩ bash for d in internal") {
			rows++
		}
	}
	if rows != 2 {
		t.Errorf("the two calls take %d rows, want one each\n%s", rows, content)
	}
}

func TestARunningCallCarriesItsElapsedTimeAndTheFooterSaysWhatIsHappening(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := liveApp(t, &at)
	early := app.View().Content
	assertGolden(t, "session-running-80x24.golden", early)
	at = at.Add(6 * time.Second)
	later := app.View().Content
	assertGolden(t, "session-running-later-80x24.golden", later)

	if early == later {
		t.Fatal("six seconds of a running call changed nothing on the screen")
	}
	plainEarly, plainLater := ansi.Strip(early), ansi.Strip(later)
	if !strings.Contains(plainEarly, "1s") || !strings.Contains(plainLater, "7s") {
		t.Errorf("the running turn does not carry its elapsed time\n--- early ---\n%s\n--- later ---\n%s", plainEarly, plainLater)
	}
	for _, frame := range []string{plainEarly, plainLater} {
		if !strings.Contains(frame, "working") {
			t.Errorf("the frame does not say what state the turn is in\n%s", frame)
		}
		if !strings.Contains(frame, "bash for d in internal") {
			t.Errorf("the running row does not say what is happening\n%s", frame)
		}
	}
	if spun(plainEarly) == spun(plainLater) {
		t.Errorf("the spinner did not move between the two moments: %q", spun(plainEarly))
	}
}

func spun(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if index := strings.IndexAny(line, spinnerFrames); index >= 0 {
			return string([]rune(line[index:])[:1])
		}
	}
	return ""
}

func TestTheModelsProseIsDrawnDifferentlyFromToolActivity(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := liveApp(t, &at)
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	content := app.View().Content
	spoken, called := styleOf(content, prose), styleOf(content, "read CLAUDE.md")
	if spoken == "" || called == "" {
		t.Fatalf("the fixture is missing a row: prose %q, tool %q\n%s", spoken, called, content)
	}
	if spoken == called {
		t.Errorf("prose and tool activity are drawn the same way: %q", spoken)
	}
	if !strings.HasPrefix(spoken, "\x1b[1;") {
		t.Errorf("the model's prose is not the heavier of the two: %q against %q", spoken, called)
	}
}

func styleOf(content, text string) string {
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(ansi.Strip(line), text) {
			continue
		}
		if end := strings.IndexByte(line, 'm'); strings.HasPrefix(line, "\x1b[") && end > 0 {
			return line[:end+1]
		}
	}
	return ""
}

func TestTheWholeCommandIsBehindAKeyAndNotOnTheScreenByDefault(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := liveApp(t, &at)
	if strings.Contains(ansi.Strip(app.View().Content), "wc -l") {
		t.Fatal("the whole command is on the screen before it was asked for")
	}
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	expanded := ansi.Strip(app.View().Content)
	if !strings.Contains(expanded, "wc -l") {
		t.Fatalf("ctrl+o did not reveal the whole command\n%s", expanded)
	}
	t.Log("\n" + expanded)
}

func TestACacheHitAndAMissRenderDifferentBottomBars(t *testing.T) {
	miss := sessionApp(t, 80, 24).View().Content
	hit := sessionApp(t, 80, 24)
	hit.Update(Event{Kind: EventStats, Model: "gpt-5.6-sol-2026-09-01", TokensIn: 284000, TokensOut: 61000, CacheRead: 9603, Decisions: 3})
	hitContent := hit.View().Content
	if miss == hitContent {
		t.Fatalf("a cache hit and a cache miss render the same frame")
	}
	if !strings.Contains(hitContent, "9k+284k/61k") {
		t.Fatalf("a cache hit does not carry the cached read beside the fresh input\n%s", hitContent)
	}
	if !strings.Contains(miss, "284k/61k") || strings.Contains(miss, "+284k") {
		t.Fatalf("a turn with no cache hit already carries a cache mark\n%s", miss)
	}
}

func TestAnAllowedCallShowsTheVerdictAndNoDistributions(t *testing.T) {
	app := sessionApp(t, 120, 36)
	content := app.View().Content
	if !strings.Contains(content, "allow") {
		t.Fatalf("the allowed call does not show its verdict\n%s", content)
	}
	head, _, _ := strings.Cut(content, "go test ./internal/judge/...")
	if strings.Contains(head, "0.04") || strings.Contains(head, "▓") {
		t.Fatalf("the allowed call drew its distributions\n%s", head)
	}
}

func TestAnAskedCallShowsEveryAnswerAndTheReason(t *testing.T) {
	content := sessionApp(t, 120, 36).View().Content
	for _, want := range []string{
		"ask", "risk", "2.00", "approval", "0.75", "user_requested", "0.11", "from_untrusted", "0.02",
		"risk 2.00 is over risk_ask_at 1.50",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("the asked call does not show %q\n%s", want, content)
		}
	}
}

func gateOffApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	for _, event := range []Event{
		{Kind: EventGateOff, Text: "there is no openrouter key"},
		{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/judge/policy/toolgate.go"},
		{Kind: EventToolResult, ID: "c1", Text: "412 lines, 11.8 KB"},
		{Kind: EventGateOff, Text: "there is no openrouter key"},
		{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: "go test ./internal/judge/..."},
		{Kind: EventToolResult, ID: "c2", Text: "ok tofu/internal/judge 0.42s"},
	} {
		app.Update(event)
	}
	return app
}

func TestWithTheGateOffTheSessionSaysSoOnceAndNoCallClaimsAVerdict(t *testing.T) {
	content := gateOffApp(t, 80, 24).View().Content
	if said := strings.Count(content, gateOffLine); said != 1 {
		t.Errorf("the session says the gate is off %d times, want 1\n%s", said, content)
	}
	for _, verdict := range []string{"  allow", "  ask", "  deny"} {
		if strings.Contains(content, verdict) {
			t.Errorf("a call claims the verdict %q with the gate off\n%s", verdict, content)
		}
	}
	assertGolden(t, "session-gate-off-80x24.golden", content)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s does not match the golden file\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestSessionViewGolden(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{"session-80x24.golden", 80, 24},
		{"session-120x36.golden", 120, 36},
	} {
		t.Run(size.name, func(t *testing.T) {
			assertGolden(t, size.name, sessionApp(t, size.width, size.height).View().Content)
		})
	}
}

func TestTheForkNoticeAppearsOnOneLineAndVanishes(t *testing.T) {
	app := sessionApp(t, 80, 24)
	quiet := app.View().Content
	app.Update(Event{Kind: EventForkStart})
	assertGolden(t, "session-forking-80x24.golden", app.View().Content)
	app.Update(Event{Kind: EventForkEnd})
	if after := app.View().Content; after != quiet {
		t.Errorf("the notice did not vanish\n--- after ---\n%s\n--- before ---\n%s", after, quiet)
	}

	absent := readGolden(t, "session-80x24.golden")
	present := readGolden(t, "session-forking-80x24.golden")
	if len(absent) != len(present) {
		t.Fatalf("the notice changed the frame from %d rows to %d", len(absent), len(present))
	}
	changed := make([]int, 0, 1)
	for row := range absent {
		if absent[row] != present[row] {
			changed = append(changed, row)
		}
	}
	if len(changed) != 1 {
		t.Fatalf("the two fixtures differ on rows %v, want one row", changed)
	}
	if !strings.Contains(present[changed[0]], forkNoticeHead) {
		t.Errorf("row %d is not the notice\n%s", changed[0], present[changed[0]])
	}
}

func readGolden(t *testing.T, name string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(body), "\n")
}

func TestTwoSubscriptionsAskNothingAndTheFirstSignedInRunsTheTurn(t *testing.T) {
	ran := make(chan string, 1)
	app := newTestApp(Options{
		Repo:   "silo",
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  bothWires,
		Turn:   func(_ context.Context, wire, _ string, _ func(Event)) { ran <- wire },
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	opened := app.View().Content
	for _, absent := range []string{"which one", "pick the wire", "1. codex", "2. anthropic"} {
		if strings.Contains(opened, absent) {
			t.Errorf("two subscriptions still ask %q\n%s", absent, opened)
		}
	}
	if !strings.Contains(opened, "openai → gpt-5.6-sol") {
		t.Errorf("the header does not name the provider and model it chose\n%s", opened)
	}
	if !strings.Contains(ansi.Strip(opened), "what should tofu do here?") {
		t.Errorf("the app did not open on a session with a composer\n%s", opened)
	}
	assertGolden(t, "chosen-80x24.golden", opened)

	typeText(app, "read one file")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case wire := <-ran:
		if wire != "codex" {
			t.Fatalf("the turn ran on %q, want codex", wire)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never started")
	}
}

func TestOneWireNamesItselfInTheHeader(t *testing.T) {
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if app.wire != "anthropic" {
		t.Fatalf("one credential left the app on wire %q", app.wire)
	}
	content := app.View().Content
	if strings.Contains(content, "codex") {
		t.Errorf("the app names a wire nobody signed in to\n%s", content)
	}
	if !strings.Contains(content, "anthropic → claude-opus-5") {
		t.Errorf("the frame does not name the only wire\n%s", content)
	}
	assertGolden(t, "session-one-wire-80x24.golden", content)
}

func crewChildren() []crew.Child {
	return []crew.Child{
		{
			Name:  "go-dev",
			Owns:  []string{"internal/judge/**", "internal/point/**"},
			Doing: "writing policy/toolgate.go",
			Since: 2*time.Minute + 14*time.Second,
			Steps: 5,
			Total: 7,
			State: crew.Running,
			Calls: []crew.Call{
				{Tool: "edit", Text: "internal/judge/policy/toolgate.go", Result: "+18 -4"},
				{Tool: "bash", Text: "go test ./internal/judge/...", Result: "ok  0.42s"},
			},
		},
		{
			Name:   "go-docs",
			Owns:   []string{"docs/**"},
			Doing:  "done, 12 files read",
			Since:  6*time.Minute + 41*time.Second,
			State:  crew.Done,
			Calls:  []crew.Call{{Tool: "read", Text: "docs/verification.md", Result: "412 lines"}},
			Report: "renamed the interface and its five implementations. one call site in point still reaches the old name through an alias.",
		},
		{
			Name:   "go-rules",
			Owns:   []string{"internal/judge/policy/**"},
			Doing:  "handed back to go-dev",
			Since:  12 * time.Second,
			State:  crew.HandedBack,
			Report: "internal/judge/policy is already held by go-dev, so the work went there.",
		},
	}
}

func crewApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := sessionApp(t, width, height)
	app.Update(Event{Kind: EventCrew, Children: crewChildren()})
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	if app.current != viewCrew {
		t.Fatalf("alt+2 left the app on view %d, want the crew view", app.current)
	}
	return app
}

func TestTheCrewViewOpensAndEscReturnsToTheSession(t *testing.T) {
	app := crewApp(t, 80, 24)
	crewFrame := app.View().Content
	assertGolden(t, "crew-80x24.golden", crewFrame)
	if !strings.Contains(crewFrame, "go-dev") {
		t.Fatalf("the crew view does not name its children\n%s", crewFrame)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.current != viewSession {
		t.Fatalf("esc left the app on view %d, want the session", app.current)
	}
	back := app.View().Content
	if !strings.Contains(back, "ok tofu/internal/judge 0.42s") {
		t.Fatalf("esc did not return to the session transcript\n%s", back)
	}
	if !strings.Contains(back, "●1") {
		t.Fatalf("the status bar does not count the one running child\n%s", back)
	}
	assertGolden(t, "crew-return-80x24.golden", back)
}

func TestTheCrewViewIsAlsoReachedByTabAndByAClick(t *testing.T) {
	app := crewApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if app.current != viewCrew {
		t.Fatalf("tab from the session reached view %d, want the crew", app.current)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	column, hit := stripColumn(app, "2 crew")
	if !hit {
		t.Fatal("the strip registered no zone for the crew view")
	}
	app.Update(tea.MouseClickMsg{X: column, Y: stripRow, Button: tea.MouseLeft})
	if app.current != viewCrew {
		t.Fatalf("a click at column %d did not select the crew view", column)
	}
}

func TestWithNoChildrenTheCrewViewSaysSoInWords(t *testing.T) {
	app := sessionApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	content := app.View().Content
	if !strings.Contains(content, "no child") {
		t.Fatalf("the empty crew view does not say there is no child\n%s", content)
	}
	if strings.Contains(content, "ownership") || strings.Contains(content, " │ ") {
		t.Fatalf("the empty crew view drew a frame instead of saying so\n%s", content)
	}
	assertGolden(t, "crew-empty-80x24.golden", content)
}

func TestCrewViewGolden(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{"crew-80x24.golden", 80, 24},
		{"crew-120x36.golden", 120, 36},
	} {
		t.Run(size.name, func(t *testing.T) {
			content := crewApp(t, size.width, size.height).View().Content
			for _, want := range []string{"go-dev", "go-docs", "go-rules", "2m 14s", "6m 41s", "12s"} {
				if !strings.Contains(content, want) {
					t.Errorf("the crew view does not show %q\n%s", want, content)
				}
			}
			assertGolden(t, size.name, content)
		})
	}
}

func TestOverlappingGlobsAreDrawnAsOneRegionRatherThanTwice(t *testing.T) {
	content := ansi.Strip(crewApp(t, 120, 36).View().Content)
	if held := strings.Count(content, "internal/judge/**"); held != 1 {
		t.Fatalf("internal/judge/** appears %d times, want once\n%s", held, content)
	}
	shared := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "internal/judge/**") {
			shared = line
		}
	}
	for _, want := range []string{"go-dev", "go-rules", "internal/judge/policy/**", "overlap"} {
		if !strings.Contains(shared, want) {
			t.Errorf("the shared region line does not carry %q\n%s", want, shared)
		}
	}
	if strings.Contains(shared, "go-docs") {
		t.Errorf("a child that shares nothing is on the shared region line\n%s", shared)
	}
	if !strings.Contains(content, "docs/**") {
		t.Errorf("the region nobody shares is missing\n%s", content)
	}
}

func TestSelectingAChildShowsItsToolCallsAndItsReport(t *testing.T) {
	app := crewApp(t, 120, 36)
	before := ansi.Strip(app.View().Content)
	if strings.Contains(before, "docs/verification.md") {
		t.Fatalf("an unselected child shows its tool calls\n%s", before)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	after := ansi.Strip(app.View().Content)
	for _, want := range []string{"docs/verification.md", "412 lines", "implementations"} {
		if !strings.Contains(after, want) {
			t.Errorf("the selected child does not show %q\n%s", want, after)
		}
	}
	if strings.Contains(after, "go test ./internal/judge/...") {
		t.Errorf("the selected child shows another child's tool call\n%s", after)
	}
	assertGolden(t, "crew-picked-120x36.golden", app.View().Content)
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if picked := ansi.Strip(app.View().Content); picked != before {
		t.Errorf("stepping back to no child changed the frame\n--- got ---\n%s\n--- want ---\n%s", picked, before)
	}
}

const openRouterKey = "sk-or-v1-77c1f0b6e5a94d2f8badc0ffee1234567890abcd"

func settingsApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   "silo",
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Providers: []settings.Provider{
			{Name: "anthropic", State: "oauth  62% of the 7d window, resets 18:00", Source: "the credential store"},
			{Name: "openrouter", Key: openRouterKey, State: "ok", Source: ".env at ~/.boji/.env"},
			{Name: "jev", State: "build jev-2026-09-01", Source: "the last decision"},
			{Name: "codex", Fix: "tofu login codex", Source: "nothing is stored"},
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	app.Update(tea.KeyPressMsg{Code: '6', Mod: tea.ModAlt})
	return app
}

func TestSettingsViewGolden(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{"settings-80x24.golden", 80, 24},
		{"settings-120x36.golden", 120, 36},
	} {
		t.Run(size.name, func(t *testing.T) {
			assertGolden(t, size.name, settingsApp(t, size.width, size.height).View().Content)
		})
	}
}

func TestSettingsNeverRendersTheWholeKey(t *testing.T) {
	content := settingsApp(t, 120, 36).View().Content
	if strings.Contains(content, openRouterKey) {
		t.Fatal("the settings view rendered the whole openrouter key")
	}
	if !strings.Contains(content, "····"+openRouterKey[len(openRouterKey)-4:]) {
		t.Fatalf("the settings view did not render the masked key\n%s", content)
	}
	for _, run := range []int{5, 8, 12} {
		if strings.Contains(content, openRouterKey[:run]) {
			t.Fatalf("the settings view rendered the first %d characters of the key", run)
		}
	}
}

func TestKeysMoveBetweenTheSessionAndSettings(t *testing.T) {
	app := settingsApp(t, 80, 24)
	if app.current != viewSettings {
		t.Fatal("alt+6 did not reach the settings view")
	}
	for _, want := range []viewID{viewSession, viewCrew, viewEdits, viewSettings} {
		app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if app.current != want {
			t.Fatalf("tab reached view %d, want %d", app.current, want)
		}
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if app.current != viewEdits {
		t.Fatalf("shift+tab reached view %d, want the file edits view", app.current)
	}
	app.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if app.current != viewSession {
		t.Fatal("the digit 1 did not select the session view")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.current != viewSession {
		t.Fatal("esc did not return to the session view")
	}
}

func TestDigitsTypeIntoTheComposer(t *testing.T) {
	app := settingsApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	typeText(app, "6")
	if app.current != viewSession {
		t.Fatal("a digit typed into the composer switched the view")
	}
	if app.view.Value() != "6" {
		t.Fatalf("the composer holds %q, want 6", app.view.Value())
	}
}

func TestClickingAViewNameSelectsIt(t *testing.T) {
	app := settingsApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	column, hit := stripColumn(app, "6 settings")
	if !hit {
		t.Fatal("the strip registered no zone for the settings view")
	}
	app.Update(tea.MouseClickMsg{X: column, Y: stripRow, Button: tea.MouseLeft})
	if app.current != viewSettings {
		t.Fatalf("a click at column %d did not select the settings view", column)
	}
	app.Update(tea.MouseClickMsg{X: column, Y: stripRow + 4, Button: tea.MouseLeft})
	if app.current != viewSettings {
		t.Fatal("a click below the strip changed the view")
	}
}

func stripColumn(app *App, label string) (int, bool) {
	index := strings.Index(ansi.Strip(app.strip.Render(app.width)), label)
	if index < 0 {
		return 0, false
	}
	return index + len(label) - 1, true
}

func setupRequirements() []Requirement {
	return []Requirement{
		{
			What: "there is no anthropic subscription credential, so no model can answer",
			Fix:  "tofu login anthropic",
		},
		{
			What: "there is no openrouter key, so no tool call is judged",
			Fix:  "tofu login openrouter",
		},
	}
}

func TestSetupViewGolden(t *testing.T) {
	remaining := setupRequirements()[1:]
	app := newTestApp(Options{
		Repo:         "silo",
		Now:          fixedClock(),
		Requirements: setupRequirements(),
		Recheck:      func() []Requirement { return remaining },
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assertGolden(t, "setup-80x24.golden", app.View().Content)

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r did not ask for a re-check")
	}
	app.Update(cmd())
	if len(app.requirements) != 1 {
		t.Fatalf("the re-check left %d requirements, want 1", len(app.requirements))
	}
	assertGolden(t, "setup-one-left-80x24.golden", app.View().Content)
}

func TestARequirementRunsItsOwnFix(t *testing.T) {
	ran := make([]string, 0, 2)
	requirements := setupRequirements()
	requirements[0].Run = func() *exec.Cmd { ran = append(ran, "anthropic"); return exec.Command("tofu", "login", "anthropic") }
	requirements[1].Run = func() *exec.Cmd { ran = append(ran, "openrouter"); return exec.Command("tofu", "login", "openrouter") }
	app := newTestApp(Options{Repo: "silo", Now: fixedClock(), Requirements: requirements})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if _, cmd := app.Update(tea.KeyPressMsg{Code: '2', Text: "2"}); cmd == nil {
		t.Fatal("2 did not run the second fix")
	}
	if len(ran) != 1 || ran[0] != "openrouter" {
		t.Fatalf("the keys ran %v, want the openrouter fix alone", ran)
	}
	if _, cmd := app.Update(tea.KeyPressMsg{Code: '3', Text: "3"}); cmd != nil {
		t.Fatal("a digit past the list ran something")
	}
}

func TestARequirementWithoutItsOwnFixFallsBackToLogin(t *testing.T) {
	ran := 0
	app := newTestApp(Options{
		Repo:         "silo",
		Now:          fixedClock(),
		Requirements: setupRequirements()[:1],
		Login:        func() *exec.Cmd { ran++; return exec.Command("tofu", "login", "anthropic") },
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if _, cmd := app.Update(tea.KeyPressMsg{Code: '1', Text: "1"}); cmd == nil {
		t.Fatal("1 did not run the login the options carry")
	}
	if ran != 1 {
		t.Fatalf("the login ran %d times, want 1", ran)
	}
}

func typeText(app *App, text string) {
	for _, code := range text {
		app.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	}
}

func TestInterruptStopsTheTurnAndKeepsTheApp(t *testing.T) {
	cancelled := make(chan struct{})
	app := newTestApp(Options{
		Repo: "silo",
		Now:  fixedClock(),
		Turn: func(ctx context.Context, _, _ string, emit func(Event)) {
			<-ctx.Done()
			close(cancelled)
			emit(Event{Kind: EventNote, Text: "stopped by the operator"})
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "rename the judge interface")

	if _, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter did not start a turn")
	}
	if !app.busy {
		t.Fatal("the app is not running a turn after enter")
	}

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("ctrl+c during a turn produced %T, want no message", msg)
		}
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("ctrl+c did not cancel the turn")
	}

	for {
		msg := app.waitForEvent()()
		app.Update(msg)
		if _, done := msg.(closedMsg); done {
			break
		}
	}
	if app.busy {
		t.Fatal("the app is still busy after the turn stopped")
	}
	app.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if app.view.Value() != "x" {
		t.Fatalf("the app stopped taking keys after ctrl+c, the composer holds %q", app.view.Value())
	}
	if app.View().Content == "" {
		t.Fatal("the app renders nothing after ctrl+c")
	}
}

func TestInterruptOutsideATurnQuits(t *testing.T) {
	app := newTestApp(Options{Repo: "silo", Now: fixedClock()})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c outside a turn produced no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("ctrl+c outside a turn did not quit")
	}
}

const (
	longSteps      = 40
	followingWords = "following"
	scrolledWords  = "scrolled back   end returns"
	narrowColumns  = 80
	narrowRows     = 24
)

func longApp(t *testing.T) *App {
	t.Helper()
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: narrowColumns, Height: narrowRows})
	for step := range longSteps {
		app.Update(Event{Kind: EventToolCall, Tool: "read", Text: "toolgate.go step " + strconv.Itoa(step)})
	}
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	return app
}

func transcriptOf(frame string) string {
	above, _, _ := strings.Cut(ansi.Strip(frame), strings.Repeat("─", narrowColumns))
	return above
}

func TestALongTranscriptScrollsALineAScreenToTheStartAndBackToTheTail(t *testing.T) {
	app := longApp(t)
	tail := app.View().Content
	assertGolden(t, "scroll-tail-80x24.golden", tail)

	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	assertGolden(t, "scroll-line-80x24.golden", app.View().Content)

	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	assertGolden(t, "scroll-screen-80x24.golden", app.View().Content)

	app.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	start := app.View().Content
	assertGolden(t, "scroll-start-80x24.golden", start)
	if !strings.Contains(transcriptOf(start), "toolgate.go step 0") {
		t.Fatalf("home did not reach the first entry\n%s", start)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if above := app.View().Content; above != start {
		t.Fatalf("scrolling above the start moved the view\n%s", above)
	}

	app.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if back := app.View().Content; back != tail {
		t.Fatalf("end did not return to the tail\n--- got ---\n%s\n--- want ---\n%s", back, tail)
	}
}

func TestEachScrollPositionDrawsItsOwnFrame(t *testing.T) {
	seen := make(map[string]string, 4)
	for _, name := range []string{
		"scroll-tail-80x24.golden",
		"scroll-line-80x24.golden",
		"scroll-screen-80x24.golden",
		"scroll-start-80x24.golden",
	} {
		frame := strings.Join(readGolden(t, name), "\n")
		if clash, same := seen[frame]; same {
			t.Errorf("%s draws the same frame as %s", name, clash)
		}
		seen[frame] = name
	}
}

func TestOutputArrivingWhileScrolledUpDoesNotMoveTheView(t *testing.T) {
	app := longApp(t)
	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	before := app.View().Content
	if !strings.Contains(ansi.Strip(before), scrolledWords) {
		t.Fatalf("the view does not say it is scrolled back\n%s", before)
	}
	app.Update(Event{Kind: EventToolCall, Tool: "bash", Text: "arrived while scrolled back"})
	after := app.View().Content
	if after != before {
		t.Fatalf("new output moved the view\n--- after ---\n%s\n--- before ---\n%s", after, before)
	}
	if strings.Contains(ansi.Strip(after), "arrived while scrolled back") {
		t.Fatalf("the scrolled view drew the new entry\n%s", after)
	}
}

func TestOutputArrivingAtTheTailMovesTheView(t *testing.T) {
	app := longApp(t)
	before := app.View().Content
	if !strings.Contains(ansi.Strip(before), followingWords) {
		t.Fatalf("the view does not say it is following\n%s", before)
	}
	app.Update(Event{Kind: EventToolCall, Tool: "bash", Text: "arrived at the tail"})
	after := app.View().Content
	if after == before {
		t.Fatal("new output at the tail left the view unmoved")
	}
	if !strings.Contains(ansi.Strip(after), "arrived at the tail") {
		t.Fatalf("the following view did not draw the new entry\n%s", after)
	}
}

func TestAShortTranscriptCannotBeScrolledAndSaysNothingAboutIt(t *testing.T) {
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: narrowColumns, Height: narrowRows})
	before := app.View().Content
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyUp}, {Code: tea.KeyPgUp}, {Code: tea.KeyHome}, {Code: tea.KeyEnd}, {Code: tea.KeyDown},
	} {
		app.Update(key)
		if after := app.View().Content; after != before {
			t.Fatalf("%s scrolled a transcript that fits\n%s", key.String(), after)
		}
	}
	for _, words := range []string{followingWords, scrolledWords} {
		if strings.Contains(ansi.Strip(before), words) {
			t.Errorf("a transcript that fits says %q\n%s", words, before)
		}
	}
	assertGolden(t, "session-one-wire-80x24.golden", before)
}

func TestTheComposerKeepsTheKeysItOwnsWhileItHasText(t *testing.T) {
	app := longApp(t)
	typeText(app, "line one")
	app.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	typeText(app, "line two")
	before := transcriptOf(app.View().Content)
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyUp}, {Code: tea.KeyHome}, {Code: tea.KeyEnd}, {Code: tea.KeyDown}} {
		app.Update(key)
	}
	if app.view.Value() != "line one\nline two" {
		t.Fatalf("the composer holds %q after the keys it owns", app.view.Value())
	}
	if after := transcriptOf(app.View().Content); after != before {
		t.Fatalf("a composer key scrolled the transcript\n--- after ---\n%s\n--- before ---\n%s", after, before)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if after := transcriptOf(app.View().Content); after == before {
		t.Fatal("pgup did not scroll while the composer had text")
	}
}

const answer = "## Tofu\n\nThe **gate** reads `toolgate.go` before the policy.\n\n" +
	"```go\nfunc Decide(answers Answers) Verdict\n```\n\n" +
	"- a question is asked once\n" +
	"- a verdict is always logged\n\n" +
	"| point | verdict |\n| --- | --- |\n| tool_gate | ask |\n"

const diagram = "The turn is a loop.\n\n```mermaid\ngraph TD\n  plan --> gate --> tool\n```\n"

func proseApp(t *testing.T, height int) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   "silo",
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Turn:   func(context.Context, string, string, func(Event)) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	return app
}

func TestProseRendersAsMarkdownWithTheGitHubExtensions(t *testing.T) {
	app := proseApp(t, 40)
	app.Update(Event{Kind: EventText, Text: answer})
	content := app.View().Content
	plain := ansi.Strip(content)
	for what, want := range map[string]string{
		"the heading":    "## Tofu",
		"the code span":  " toolgate.go ",
		"the fenced go":  "func Decide(answers Answers) Verdict",
		"the list":       "• a question is asked once",
		"the table rule": "─┼─",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("%s is missing, want %q in\n%s", what, want, plain)
		}
	}
	if strings.Contains(plain, "**gate**") || strings.Contains(plain, "`toolgate.go`") {
		t.Errorf("the markdown is shown raw\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[38;5;255;1mgate\x1b[m") {
		t.Errorf("the bold span is not bold\n%q", content)
	}
	assertGolden(t, "session-markdown-80x40.golden", content)
}

func TestAMermaidFenceIsACodeBlockAndNotADiagram(t *testing.T) {
	app := proseApp(t, 24)
	app.Update(Event{Kind: EventText, Text: diagram})
	content := app.View().Content
	plain := ansi.Strip(content)
	for _, want := range []string{"graph TD", "plan --> gate --> tool"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the mermaid source is not readable, want %q in\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "```") {
		t.Errorf("the fence markers are shown\n%s", plain)
	}
	assertGolden(t, "session-mermaid-80x24.golden", content)
}

func TestAStreamingMessageIsPlainAndTheCompleteOneIsMarkdown(t *testing.T) {
	app := proseApp(t, 24)
	app.Update(Event{Kind: EventTextDelta, Text: "The **gate** reads "})
	app.Update(Event{Kind: EventTextDelta, Text: "`toolgate.go` before the policy.\n"})
	streaming := app.View().Content
	if !strings.Contains(ansi.Strip(streaming), "The **gate** reads `toolgate.go` before the policy.") {
		t.Errorf("a streaming message is not shown as plain text\n%s", ansi.Strip(streaming))
	}
	assertGolden(t, "session-streaming-80x24.golden", streaming)

	app.Update(closedMsg{})
	complete := app.View().Content
	if streaming == complete {
		t.Fatal("the message was not re-rendered when it stopped")
	}
	if strings.Contains(ansi.Strip(complete), "**gate**") {
		t.Errorf("the complete message is still raw\n%s", ansi.Strip(complete))
	}
	assertGolden(t, "session-complete-80x24.golden", complete)
}

func TestTheWheelScrollsTheTranscriptAndOnlyInTheSessionView(t *testing.T) {
	app := longApp(t)
	tail := app.View().Content
	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if app.View().Content == tail {
		t.Fatal("the wheel did not scroll the transcript")
	}
	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if back := app.View().Content; back != tail {
		t.Fatalf("the wheel did not return to the tail\n%s", back)
	}
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	crewFrame := app.View().Content
	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if app.View().Content != crewFrame {
		t.Fatal("the wheel scrolled while another view was open")
	}
}

func TestTheSetupScreenDrawsNoStatusBar(t *testing.T) {
	setup := newTestApp(Options{Repo: "silo", Now: fixedClock(), Requirements: setupRequirements()})
	setup.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	running := newTestApp(Options{Repo: "silo", Now: fixedClock(), Wires: anthropicAlone})
	running.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	content := setup.View().Content
	for _, absent := range []string{"context unread", "quota unread", "⇅ 0/0", "jev 0"} {
		if strings.Contains(content, absent) {
			t.Errorf("setup draws %q with no session behind it\n%s", absent, content)
		}
	}
	if rows := strings.Count(content, "\n") + 1; rows != 24 {
		t.Errorf("setup fills %d rows of the 24 it was given", rows)
	}
	if content := running.View().Content; !strings.Contains(content, "jev 0") {
		t.Errorf("the session view lost the status bar it still needs\n%s", content)
	}
}
