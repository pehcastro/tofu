package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/pick"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
	"tofu/interface/tui/work"
	"tofu/internal/golden"
	"tofu/internal/judge/jev"
	"tofu/internal/llm"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
)

const (
	testRelease = fixture.Release
	testRepo    = fixture.Path
)

func newTestApp(options Options) *App {
	options.Release = testRelease
	app := New(options)
	app.Update(Event{Kind: EventSession, Text: fixture.SessionName, ID: fixture.SessionID})
	return app
}

func fixedClock() func() time.Time {
	at := fixture.Opened().Add(138 * time.Second)
	return func() time.Time { return at }
}

func containsAPlaceholder(text string) bool {
	return strings.Contains(text, session.Placeholder)
}

func claudeEfforts() []llm.Effort {
	return []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax}
}

func bothWires() []Wire {
	return []Wire{
		{Name: "codex", Model: "gpt-5.6-sol", Provider: "codex-sub", Efforts: llm.Efforts()},
		{Name: "anthropic", Model: "claude-opus-5", Provider: "claude-sub", Efforts: claudeEfforts()},
	}
}

func anthropicAlone() []Wire {
	return []Wire{{Name: "anthropic", Model: "claude-opus-5", Provider: "claude-sub", Efforts: claudeEfforts()}}
}

func sessionApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	app.Update(fixture.Quotas(fixedClock()()))
	for _, event := range []Event{
		{Kind: EventContext, Context: fixture.Context()},
		{Kind: EventStats, TokensIn: 284000, TokensOut: 61000, Decisions: 3},
		{Kind: EventText, Text: "reading the gate first, then the policy that decides it."},
		{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/judge/policy/toolgate.go"},
		{Kind: EventDecision, Decision: allowed()},
		{Kind: EventToolResult, ID: "c1", Text: "412 lines, 11.8 KB"},
		{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: "go test ./internal/judge/..."},
		{Kind: EventDecision, Decision: asked()},
		{Kind: EventToolResult, ID: "c2", Text: "ok tofu/internal/judge 0.42s"},
		{Kind: EventDone, Text: "cooked for"},
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
		Reason: session.Reason{
			Question:  "risk",
			Limit:     "risk_ask_at",
			Levels:    fixture.RiskLevels(),
			Threshold: 1.5,
			Value:     2,
		},
	}
}

const (
	longCommand = `cd /home/dev/tofu; for d in internal/* interface/* cmd/* library/*; ` +
		`do n=$(find "$d" -name '*.go' | wc -l); echo "$d $n"; done`
	longIntent     = "for d in internal/* interface/* cmd/* +3 more"
	forkNoticeHead = "⟳ forking"
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
	golden.Assert(t, "session-running-80x24.golden", early)
	at = at.Add(6 * time.Second)
	later := app.View().Content
	golden.Assert(t, "session-running-later-80x24.golden", later)

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
	app.Update(Event{Kind: EventDecision, Decision: asked()})
	content := app.View().Content
	spoken, called := styleOf(content, prose), styleOf(content, "bash "+longIntent)
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

func TestSlashWorkRevealsTheWholeCommandLikeCtrlO(t *testing.T) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := liveApp(t, &at)
	typeText(app, "/work")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != viewWork {
		t.Fatal("/work did not switch to the work view")
	}
	expanded := ansi.Strip(app.View().Content)
	if !strings.Contains(expanded, "wc -l") {
		t.Fatalf("/work did not reveal the whole command\n%s", expanded)
	}
}

func TestACacheHitAndAMissRenderDifferentBottomBars(t *testing.T) {
	miss := sessionApp(t, 80, 24).View().Content
	hit := sessionApp(t, 80, 24)
	hit.Update(Event{Kind: EventStats, TokensIn: 284000, TokensOut: 61000, CacheRead: 9603, Decisions: 3})
	hitContent := hit.View().Content
	if miss == hitContent {
		t.Fatalf("a cache hit and a cache miss render the same frame")
	}
	if !strings.Contains(hitContent, "284k read  61k write  9k cached") {
		t.Fatalf("a cache hit does not carry the cached read beside the fresh input\n%s", hitContent)
	}
	if !strings.Contains(miss, "284k read  61k write") || strings.Contains(miss, "cached") {
		t.Fatalf("a turn with no cache hit already carries a cache mark\n%s", miss)
	}
}

func TestAnAllowedCallFoldsInChatAndCarriesNoVerdictOrDistributions(t *testing.T) {
	app := sessionApp(t, 120, 36)
	content := app.View().Content
	if strings.Contains(content, "  allow") {
		t.Fatalf("the allowed call still shows its verdict in chat\n%s", content)
	}
	if strings.Contains(content, "0.04") {
		t.Fatalf("the allowed call drew its distributions in chat\n%s", content)
	}
	if !strings.Contains(content, "(1) tools · jev 1") {
		t.Fatalf("the allowed call did not fold into the turn's one running line\n%s", content)
	}
}

func TestAnAskedCallShowsEveryAnswerAndTheReason(t *testing.T) {
	content := sessionApp(t, 120, 36).View().Content
	for _, want := range []string{
		"ask", "risk", "2.00", "approval", "0.75", "user_requested", "0.11", "from_untrusted", "0.02",
		riskSentence,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("the asked call does not show %q\n%s", want, content)
		}
	}
}

func keyEnvPath() string {
	return `C:\Users\Luiz\AppData\Local\Temp\orch-drive\home\.tofu\.env`
}

func noKeyAtAll() string {
	return `jev.Key: missing_credential: OPENROUTER_KEY is not set and ` + keyEnvPath() + ` does not exist`
}

func gateOffApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	for _, event := range []Event{
		{Kind: EventGateOff, Text: noKeyAtAll(), GateWhy: jev.WhyNoFile},
		{Kind: EventToolCall, ID: "c1", Tool: "read", Text: "internal/judge/policy/toolgate.go"},
		{Kind: EventToolResult, ID: "c1", Text: "412 lines, 11.8 KB"},
		{Kind: EventGateOff, Text: noKeyAtAll(), GateWhy: jev.WhyNoFile},
		{Kind: EventToolCall, ID: "c2", Tool: "bash", Text: "go test ./internal/judge/..."},
		{Kind: EventToolResult, ID: "c2", Text: "ok tofu/internal/judge 0.42s"},
	} {
		app.Update(event)
	}
	return app
}

func TestTheGateOffNoteKeepsThePathAndTheFunctionOutOfTheTranscript(t *testing.T) {
	content := ansi.Strip(gateOffApp(t, 80, 24).View().Content)
	for _, absent := range []string{keyEnvPath(), `\.tofu\.env`, "jev.Key", "missing_credential"} {
		if strings.Contains(content, absent) {
			t.Errorf("the transcript pastes %q from the raw error\n%s", absent, content)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(content), " "), gateOffNoKey) {
		t.Errorf("the transcript does not say what to do about the missing key\n%s", content)
	}
}

type gateOffState struct {
	why  jev.Why
	want string
}

func gateOffStates() []gateOffState {
	return []gateOffState{
		{jev.WhyNoFile, gateOffNoKey},
		{jev.WhyFileLacksName, gateOffKeyUnnamed},
		{jev.WhyUnreadable, gateOffKeyUnread},
		{jev.WhyUnexplained, gateOffUnexplained},
	}
}

func TestEachGateOffStateGetsItsOwnLine(t *testing.T) {
	for _, state := range gateOffStates() {
		if note := gateOffNote(state.why); note != gateOffLine+" "+state.want {
			t.Errorf("reason %d reads as %q, want the line ending %q", state.why, note, state.want)
		}
	}
}

func TestARewordedKeyErrorStillPicksTheSentenceTheReasonAsksFor(t *testing.T) {
	reworded := `jev.Key: missing_credential: nothing anywhere holds OPENROUTER_KEY, looked at ` + keyEnvPath()
	for _, state := range gateOffStates() {
		app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
		app.Init()
		app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		app.Update(Event{Kind: EventGateOff, Text: reworded, GateWhy: state.why})
		content := ansi.Strip(app.View().Content)
		if flat := strings.Join(strings.Fields(content), " "); !strings.Contains(flat, gateOffLine+" "+state.want) {
			t.Errorf("reason %d shows the wrong sentence, want %q\n%s", state.why, state.want, content)
		}
	}
}

func TestTheRawGateErrorIsWholeInTheWorkView(t *testing.T) {
	app := gateOffApp(t, 80, 24)
	found := false
	for _, entry := range app.work.Entries {
		found = found || (entry.Head == gateOffHead && entry.Output == noKeyAtAll())
	}
	if !found {
		t.Fatalf("no work entry carries the raw gate error\n%+v", app.work.Entries)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "missing_credential") || !strings.Contains(content, gateOffHead) {
		t.Errorf("the work view does not show the raw gate error\n%s", content)
	}
}

func TestTheAnswerDoesNotPointAtTheGateNoteForItsWork(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(Event{Kind: EventGateOff, Text: noKeyAtAll()})
	if id := app.turnWorkID(); id != "" {
		t.Errorf("the answer points at the gate note %q as the work of the turn", id)
	}
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
	golden.Assert(t, "session-gate-off-80x24.golden", content)
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
			golden.Assert(t, size.name, sessionApp(t, size.width, size.height).View().Content)
		})
	}
}

func TestTheForkNoticeAppearsOnOneLineAndVanishes(t *testing.T) {
	app := sessionApp(t, 80, 24)
	quiet := app.View().Content
	app.Update(Event{Kind: EventForkStart})
	golden.Assert(t, "session-forking-80x24.golden", app.View().Content)
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
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  bothWires,
		Turn: func(_ context.Context, pick Pick, _ string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			ran <- pick.Wire
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	opened := app.View().Content
	for _, absent := range []string{"which one", "pick the wire", "1. codex", "2. anthropic"} {
		if strings.Contains(opened, absent) {
			t.Errorf("two subscriptions still ask %q\n%s", absent, opened)
		}
	}
	if !strings.Contains(opened, "codex-sub/gpt-5.6-sol") {
		t.Errorf("the header does not name the subscription and model it chose\n%s", opened)
	}
	if !containsAPlaceholder(ansi.Strip(opened)) {
		t.Errorf("the app did not open on a session with a composer\n%s", opened)
	}
	golden.Assert(t, "chosen-80x24.golden", opened)

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
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if app.wire != "anthropic" {
		t.Fatalf("one credential left the app on wire %q", app.wire)
	}
	content := app.View().Content
	if strings.Contains(content, "codex") {
		t.Errorf("the app names a wire nobody signed in to\n%s", content)
	}
	if !strings.Contains(content, "claude-sub/claude-opus-5") {
		t.Errorf("the frame does not name the only wire\n%s", content)
	}
	golden.Assert(t, "session-one-wire-80x24.golden", content)
}

func subAgentChildren() []subagent.Child {
	return []subagent.Child{
		{
			Name:  "go-dev",
			Owns:  []string{"internal/judge/**", "internal/point/**"},
			Doing: "writing policy/toolgate.go",
			Since: 2*time.Minute + 14*time.Second,
			Steps: 5,
			Total: 7,
			State: roster.Working,
			Calls: []subagent.Call{
				{Tool: "edit", Text: "internal/judge/policy/toolgate.go", Result: "+18 -4"},
				{Tool: "bash", Text: "go test ./internal/judge/...", Result: "ok  0.42s"},
			},
		},
		{
			Name:   "go-docs",
			Owns:   []string{"docs/**"},
			Doing:  "done, 12 files read",
			Since:  6*time.Minute + 41*time.Second,
			State:  roster.Finished,
			Calls:  []subagent.Call{{Tool: "read", Text: "docs/verification.md", Result: "412 lines"}},
			Report: "renamed the interface and its five implementations. one call site in point still reaches the old name through an alias.",
		},
		{
			Name:   "go-rules",
			Owns:   []string{"internal/judge/policy/**"},
			Doing:  "handed back to go-dev",
			Since:  12 * time.Second,
			State:  roster.InReview,
			Report: "internal/judge/policy is already held by go-dev, so the work went there.",
		},
	}
}

func subAgentApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := sessionApp(t, width, height)
	app.Update(Event{Kind: EventSubAgent, Children: subAgentChildren()})
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	if app.current != viewSubAgents {
		t.Fatalf("alt+4 left the app on view %d, want the sub-agent view", app.current)
	}
	return app
}

func TestTheSubAgentViewOpensAndEscReturnsToTheSession(t *testing.T) {
	app := subAgentApp(t, 80, 24)
	subAgentFrame := app.View().Content
	golden.Assert(t, "subagent-80x24.golden", subAgentFrame)
	if !strings.Contains(subAgentFrame, "go-dev") {
		t.Fatalf("the sub-agent view does not name its children\n%s", subAgentFrame)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.current != viewChat {
		t.Fatalf("esc left the app on view %d, want chat", app.current)
	}
	back := app.View().Content
	if !strings.Contains(back, "ok tofu/internal/judge 0.42s") {
		t.Fatalf("esc did not return to the session transcript\n%s", back)
	}
	if !strings.Contains(back, "●1") {
		t.Fatalf("the status bar does not count the one running child\n%s", back)
	}
	golden.Assert(t, "subagent-return-80x24.golden", back)
}

func TestTheSubAgentViewIsAlsoReachedByTabAndByAClick(t *testing.T) {
	app := subAgentApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	for range 3 {
		app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if app.current != viewSubAgents {
		t.Fatalf("three tabs from chat reached view %d, want the sub-agent view", app.current)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	column, hit := stripColumn(app, "[4] sub-agents")
	if !hit {
		t.Fatal("the strip registered no zone for the sub-agents view")
	}
	click(app, pick.Cell{X: column, Y: stripRow})
	if app.current != viewSubAgents {
		t.Fatalf("a click at column %d did not select the sub-agents view", column)
	}
}

func TestWithNoChildrenTheSubAgentViewSaysSoInWords(t *testing.T) {
	app := sessionApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	content := app.View().Content
	if !strings.Contains(content, "no child") {
		t.Fatalf("the empty sub-agent view does not say there is no child\n%s", content)
	}
	if strings.Contains(content, "ownership") || strings.Contains(content, " │ ") {
		t.Fatalf("the empty sub-agent view drew a frame instead of saying so\n%s", content)
	}
	golden.Assert(t, "subagent-empty-80x24.golden", content)
}

func TestSubAgentViewGolden(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{"subagent-80x24.golden", 80, 24},
		{"subagent-120x36.golden", 120, 36},
	} {
		t.Run(size.name, func(t *testing.T) {
			content := subAgentApp(t, size.width, size.height).View().Content
			for _, want := range []string{"go-dev", "go-docs", "go-rules", "2m 14s", "6m 41s", "12s"} {
				if !strings.Contains(content, want) {
					t.Errorf("the sub-agent view does not show %q\n%s", want, content)
				}
			}
			golden.Assert(t, size.name, content)
		})
	}
}

func TestOverlappingGlobsAreDrawnAsOneRegionRatherThanTwice(t *testing.T) {
	content := ansi.Strip(subAgentApp(t, 120, 36).View().Content)
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

func TestTheSubAgentWatchPaneDrawsTheRunningChildsProgressLine(t *testing.T) {
	app := subAgentApp(t, 120, 36)
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	frame := ansi.Strip(app.View().Content)
	if !strings.Contains(frame, "writing policy/toolgate.go") {
		t.Fatalf("the watch pane does not draw the running child's progress line\n%s", frame)
	}
	golden.Assert(t, "subagent-watch-running-120x36.golden", app.View().Content)
}

func TestSelectingAChildShowsItsToolCallsAndItsReport(t *testing.T) {
	app := subAgentApp(t, 120, 36)
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
	golden.Assert(t, "subagent-picked-120x36.golden", app.View().Content)
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if picked := ansi.Strip(app.View().Content); picked != before {
		t.Errorf("stepping back to no child changed the frame\n--- got ---\n%s\n--- want ---\n%s", picked, before)
	}
}

func childOf(report string) []subagent.Child {
	return []subagent.Child{{Name: "c1", Owns: []string{"note.txt"}, Doing: "read note.txt", State: roster.Finished, Report: report}}
}

func TestAChildsMessageIsDrawnInTheSubAgentsPanelAndNeverSpokenInTheParentsTranscript(t *testing.T) {
	const childSaid, parentSaid = "the note holds one line", "the child read it for me"
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	for _, event := range []Event{
		{Kind: EventText, Text: "handing it to a child"},
		{Kind: EventToolCall, ID: "s1", Tool: "spawn", Text: "read note.txt", Promote: true},
		{Kind: EventTextDelta, Text: childSaid},
		{Kind: EventToolResult, ID: "s1", Text: "spawn c1 finished"},
		{Kind: EventSubAgent, Children: childOf(childSaid)},
		{Kind: EventText, Text: parentSaid},
		{Kind: EventDone, Text: "cooked for"},
	} {
		app.Update(event)
	}
	transcript := ansi.Strip(app.View().Content)
	if strings.Contains(transcript, childSaid) {
		t.Errorf("the child spoke in the parent's transcript with nothing saying it was the child\n%s", transcript)
	}
	if !strings.Contains(transcript, parentSaid) {
		t.Fatalf("the parent's own message is missing from its transcript\n%s", transcript)
	}
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	panel := ansi.Strip(app.View().Content)
	for _, want := range []string{"c1", childSaid} {
		if !strings.Contains(panel, want) {
			t.Errorf("the sub-agents panel does not carry %q\n%s", want, panel)
		}
	}
}

func afterAFullEventChannel(afterwards ...Event) (*App, []Event) {
	filled := make(chan struct{})
	app := newTestApp(Options{
		Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires,
		Turn: func(_ context.Context, _ Pick, _ string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
			for range eventBuffer * 2 {
				emit(Event{Kind: EventContext, Context: fixture.Context()})
			}
			close(filled)
			for _, event := range afterwards {
				emit(event)
			}
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	for _, letter := range "read the note" {
		app.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	_, started := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	<-filled
	var delivered []Event
	for pending := []tea.Cmd{started}; len(pending) > 0; pending = pending[1:] {
		if pending[0] == nil {
			continue
		}
		switch msg := pending[0]().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case Event:
			delivered = append(delivered, msg)
			_, next := app.Update(msg)
			pending = append(pending, next)
		}
	}
	return app, delivered
}

func TestTheLastSubAgentStateReachesThePanelEvenWhenTheChannelIsFull(t *testing.T) {
	const childSaid = "the note holds one line"
	app, _ := afterAFullEventChannel(Event{Kind: EventSubAgent, Children: childOf(childSaid)})
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if panel := ansi.Strip(app.View().Content); !strings.Contains(panel, childSaid) {
		t.Fatalf("a full channel threw away the child's last state, so the panel never showed it\n%s", panel)
	}
}

func TestBothForkNoticesReachTheScreenWhenTheChannelIsFull(t *testing.T) {
	app, _ := afterAFullEventChannel(Event{Kind: EventForkStart})
	if screen := ansi.Strip(app.View().Content); !strings.Contains(screen, forkNoticeHead) {
		t.Fatalf("a full channel threw away the fork notice, so the screen never said a fork began\n%s", screen)
	}

	ended, _ := afterAFullEventChannel(Event{Kind: EventForkStart}, Event{Kind: EventForkEnd})
	if screen := ansi.Strip(ended.View().Content); strings.Contains(screen, forkNoticeHead) {
		t.Fatalf("a full channel threw away the end of the fork, so the notice stayed on the screen for a fork that finished\n%s", screen)
	}
}

func TestASupersededContextValueIsStillDroppedWhenTheChannelIsFull(t *testing.T) {
	_, delivered := afterAFullEventChannel()
	kept := 0
	for _, event := range delivered {
		if event.Kind == EventContext {
			kept++
		}
	}
	if kept != eventBuffer {
		t.Fatalf("the turn sent %d context values and %d reached the app, want the %d the channel holds", eventBuffer*2, kept, eventBuffer)
	}
}

const openRouterKey = "sk-or-v1-77c1f0b6e5a94d2f8badc0ffee1234567890abcd"

func settingsApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Providers: []settings.Provider{
			{Name: "claude-sub", State: "oauth  62% of the 7d window, resets 18:00", Source: "the credential store"},
			{Name: "openrouter", Key: openRouterKey, State: "ok", Source: ".env at ~/.tofu/.env"},
			{Name: "jev", State: "build jev-2026-09-01", Source: "the last decision"},
			{Name: "codex-sub", Fix: "tofu login codex-sub", Source: "nothing is stored"},
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	app.runCommand("settings")
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
			golden.Assert(t, size.name, settingsApp(t, size.width, size.height).View().Content)
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

func TestTheSettingsCommandReachesSettingsAndEscReturnsToChat(t *testing.T) {
	app := settingsApp(t, 80, 24)
	if app.current != viewSettings {
		t.Fatal("the settings command did not reach the settings view")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.current != viewChat {
		t.Fatal("esc did not leave settings for chat")
	}
}

func settingsStoreApp(t *testing.T, globalPath string) *App {
	t.Helper()
	store, err := isettings.Open(globalPath, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Settings: store})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.runCommand("settings")
	return app
}

func TestASettingChangedInTheRunningInterfaceIsOnDiskBeforeTheNextKeystroke(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "settings.json")
	app := settingsStoreApp(t, globalPath)
	app.settingsKey("down")
	app.settingsKey("space")

	raw, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read the settings file written by the toggle: %v", err)
	}
	if !strings.Contains(string(raw), `"chatShowsTools": 1`) {
		t.Fatalf("the toggle is not on disk before the next keystroke\n%s", raw)
	}
}

func TestASettingSurvivesARestartOfTheInterface(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "settings.json")
	app := settingsStoreApp(t, globalPath)
	app.settingsKey("down")
	app.settingsKey("down")
	for range 3 {
		app.settingsKey("right")
	}
	if got := app.settingsStore.Int(isettings.DecisionCap); got != 3 {
		t.Fatalf("decisionCap after three increments = %d, want 3", got)
	}

	restarted := settingsStoreApp(t, globalPath)
	if got := restarted.settingsStore.Int(isettings.DecisionCap); got != 3 {
		t.Fatalf("a rebuilt model over the same path reads decisionCap = %d, want 3", got)
	}
}

func TestChatShowsToolsPersistsAcrossARestart(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "settings.json")
	app := settingsStoreApp(t, globalPath)
	app.settingsKey("down")
	app.settingsKey("space")
	if !app.settings.ChatShowsTools {
		t.Fatal("toggling the row did not update the model's effective flag")
	}

	restarted := settingsStoreApp(t, globalPath)
	if !restarted.settings.ChatShowsTools {
		t.Fatal("chatShowsTools did not survive a restart")
	}
}

func TestSpacePressedOnTheKeyboardTogglesASettingsRow(t *testing.T) {
	spacePress := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	if got := spacePress.String(); got != "space" {
		t.Fatalf("a space press stringifies to %q, so this test is not sending what a keyboard sends", got)
	}
	app := settingsStoreApp(t, filepath.Join(t.TempDir(), "settings.json"))
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	app.Update(spacePress)
	if !app.settingsStore.Bool(isettings.ChatShowsTools) {
		t.Fatal("space through App.Update did not toggle the row under the cursor")
	}
}

func TestFoldHidesShellIsASettingThatDropsTheShellCountFromTheRunningLine(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "settings.json")
	app := settingsStoreApp(t, globalPath)
	app.runCommand("chat")
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "go test ./..."})
	before := ansi.Strip(app.View().Content)
	if !strings.Contains(before, "shell (1)") {
		t.Fatalf("a running shell call does not carry a shell count before the setting is touched\n%s", before)
	}

	app.runCommand("settings")
	for range 3 {
		app.settingsKey("down")
	}
	app.settingsKey("space")
	if !app.settingsStore.Bool(isettings.FoldHidesShell) {
		t.Fatal("toggling the row did not flip foldHidesShell on disk")
	}

	app.runCommand("chat")
	after := ansi.Strip(app.View().Content)
	if strings.Contains(after, "shell (1)") {
		t.Fatalf("the running line still carries a shell count once foldHidesShell is on\n%s", after)
	}
}

var settingsSearchReportsACount = regexp.MustCompile(`search: tool  \d+ matches?`)

func TestTheSettingsViewTakesTheCursorScopeAndSearchKeys(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "settings.json")
	app := settingsStoreApp(t, globalPath)
	for _, letter := range "tool" {
		app.settingsKey(string(letter))
	}
	content := app.View().Content
	if !strings.Contains(content, "search: tool") {
		t.Fatalf("the settings view does not show the search query\n%s", content)
	}
	if !strings.Contains(content, "chat shows every tool call") {
		t.Fatalf("the search did not keep the matching row\n%s", content)
	}
	if strings.Contains(content, "decision cap") {
		t.Fatalf("the search kept a row that does not match\n%s", content)
	}
	if !settingsSearchReportsACount.MatchString(content) {
		t.Fatalf("the settings view does not report how many rows the search found\n%s", content)
	}

	app.settingsKey("down")
	app.settingsKey("space")
	if !app.settingsStore.Bool(isettings.ChatShowsTools) {
		t.Fatal("space did not toggle the matched row through the cursor")
	}
}

func TestReloadAppearsInTheCommandMenuAndCallsTheVerb(t *testing.T) {
	called := false
	app := newTestApp(Options{
		Repo: testRepo,
		Now:  fixedClock(),
		Reload: func() string {
			called = true
			return "reloaded 4 rules"
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	found := false
	for _, command := range commands(app.options) {
		if command.Name == "reload" {
			found = true
		}
	}
	if !found {
		t.Fatal("reload is not offered in the command menu")
	}

	app.runCommand("reload")
	if !called {
		t.Fatal("/reload did not call the verb")
	}
	if !strings.Contains(app.View().Content, "reloaded 4 rules") {
		t.Fatalf("the reload result is not shown in chat\n%s", app.View().Content)
	}
}

func TestNoBottomBarDrawsOnTheSubAgentsView(t *testing.T) {
	app := subAgentApp(t, 80, 24)
	content := app.View().Content
	for _, absent := range []string{"/250k", "jev 3", "tofu " + testRelease} {
		if strings.Contains(content, absent) {
			t.Fatalf("the sub-agents view still draws %q from the bottom bar\n%s", absent, content)
		}
	}
	golden.Assert(t, "subagent-no-bar-80x24.golden", content)
}

func TestTabCyclesTheFiveMainViews(t *testing.T) {
	app := sessionApp(t, 80, 24)
	for _, want := range []viewID{viewWork, viewEdits, viewSubAgents, viewShells, viewChat} {
		app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if app.current != want {
			t.Fatalf("tab reached view %d, want %d", app.current, want)
		}
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if app.current != viewShells {
		t.Fatalf("shift+tab reached view %d, want shells", app.current)
	}
	app.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if app.current != viewChat {
		t.Fatal("the digit 1 did not select the chat view")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.current != viewChat {
		t.Fatal("esc did not return to the chat view")
	}
}

func TestDigitsTypeIntoTheComposer(t *testing.T) {
	app := settingsApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	typeText(app, "6")
	if app.current != viewChat {
		t.Fatal("a digit typed into the composer switched the view")
	}
	if app.view.Value() != "6" {
		t.Fatalf("the composer holds %q, want 6", app.view.Value())
	}
}

func TestClickingAViewNameSelectsIt(t *testing.T) {
	app := sessionApp(t, 80, 24)
	column, hit := stripColumn(app, "[5] shells")
	if !hit {
		t.Fatal("the strip registered no zone for the shells view")
	}
	click(app, pick.Cell{X: column, Y: stripRow})
	if app.current != viewShells {
		t.Fatalf("a click at column %d did not select the shells view", column)
	}
	click(app, pick.Cell{X: column, Y: stripRow + 4})
	if app.current != viewShells {
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
			What: "there is no claude-sub subscription credential, so no model can answer",
			Fix:  "tofu login claude-sub",
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
		Repo:         testRepo,
		Now:          fixedClock(),
		Requirements: setupRequirements(),
		Recheck:      func() []Requirement { return remaining },
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	golden.Assert(t, "setup-80x24.golden", app.View().Content)

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r did not ask for a re-check")
	}
	app.Update(cmd())
	if len(app.requirements) != 1 {
		t.Fatalf("the re-check left %d requirements, want 1", len(app.requirements))
	}
	golden.Assert(t, "setup-one-left-80x24.golden", app.View().Content)
}

func TestARequirementRunsItsOwnFix(t *testing.T) {
	ran := make([]string, 0, 2)
	requirements := setupRequirements()
	requirements[0].Run = func() *exec.Cmd { ran = append(ran, "anthropic"); return exec.Command("tofu", "login", "anthropic") }
	requirements[1].Run = func() *exec.Cmd { ran = append(ran, "openrouter"); return exec.Command("tofu", "login", "openrouter") }
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Requirements: requirements})
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
		Repo:         testRepo,
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
		Repo: testRepo,
		Now:  fixedClock(),
		Turn: func(ctx context.Context, _ Pick, _ string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
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
		if _, done := msg.(Closed); done {
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

func TestInterruptOutsideATurnAsksBeforeItQuits(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock()})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("one ctrl+c at an idle prompt produced %T, want the program still running", cmd())
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, quitAgainNote) {
		t.Fatalf("one ctrl+c at an idle prompt asked nothing\n%s", plain)
	}
	_, cmd = app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("a second ctrl+c produced no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("a second ctrl+c did not quit")
	}
}

func TestTypingBetweenTwoInterruptsKeepsTheProgramRunning(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock()})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	typeText(app, "no")
	if _, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("typing between the two presses still quit with %T", cmd())
	}
}

var (
	shellCall = Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "sleep 3"}
	childCall = Event{Kind: EventToolCall, ID: "c1", Tool: "spawn", Text: "write half a file", Promote: true}
)

func callingApp(t *testing.T, call Event) (*App, context.Context) {
	t.Helper()
	contexts := make(chan context.Context, 1)
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Turn: func(ctx context.Context, _ Pick, _ string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			contexts <- ctx
			<-ctx.Done()
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "read the changelog")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var held context.Context
	select {
	case held = <-contexts:
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never started")
	}
	app.Update(call)
	return app, held
}

func TestTheFirstInterruptLeavesTheRunningToolCallAlive(t *testing.T) {
	app, held := callingApp(t, shellCall)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	select {
	case <-held.Done():
		t.Fatal("the first ctrl+c cancelled the context the running tool call holds")
	case <-time.After(100 * time.Millisecond):
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, stoppingModel) {
		t.Fatalf("the screen does not say the running tools are being let finish\n%s", plain)
	}
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "done"})
	select {
	case <-held.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the turn ran on after the tool call it was waiting on reported")
	}
}

func TestASecondInterruptStopsTheRunningToolCallToo(t *testing.T) {
	app, held := callingApp(t, shellCall)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	select {
	case <-held.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a second ctrl+c did not cancel the context the running tool call holds")
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, stoppingNote) {
		t.Fatalf("the screen does not say the turn was stopped\n%s", plain)
	}
}

func TestASecondInterruptStopsTheTurnHoweverLongAfterTheFirstItArrives(t *testing.T) {
	app, held := callingApp(t, shellCall)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	app.pressedAt = app.pressedAt.Add(-time.Hour)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	select {
	case <-held.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a second ctrl+c an hour after the first stopped nothing and said nothing either")
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, stoppingNote) {
		t.Fatalf("the screen does not say the turn was stopped\n%s", plain)
	}
}

func TestOneInterruptReachesAChildRunningInsideTheSpawnCall(t *testing.T) {
	app, held := callingApp(t, childCall)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	select {
	case <-held.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("one ctrl+c left the child running: the spawn call was let finish like a shell command")
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, stoppingNote) {
		t.Fatalf("the screen does not say the turn was stopped\n%s", plain)
	}
}

func TestAChildStopsReadingAsRunningOnceTheTurnHasEnded(t *testing.T) {
	app, _ := callingApp(t, childCall)
	app.Update(Event{Kind: EventSubAgent, Children: []subagent.Child{
		{Name: "c1", Doing: "write half a file", State: roster.Working},
		{Name: "c2", Doing: "read the changelog", State: roster.WaitingAnswer},
	}})
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	app.Update(Closed{})
	if running := app.subagents.Running(); running != 0 {
		t.Fatalf("the panel still counts %d children running after the turn ended", running)
	}
	for _, child := range app.subagents.Children {
		if child.State != roster.Parked {
			t.Fatalf("%s reads as %s after the stop, want parked", child.Name, subagent.Label(child.State))
		}
	}
	app.show(viewSubAgents)
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, "2 children, 0 running") {
		t.Fatalf("the sub-agents panel goes on claiming a running child\n%s", plain)
	}
}

func TestTheFooterNamesWhatTheSecondInterruptDoesWhileToolsFinish(t *testing.T) {
	const (
		stopsTheTurn  = "ctrl+c stops the turn"
		stopsTheTools = "letting the running tools finish, ctrl+c again stops them"
	)
	app, _ := callingApp(t, shellCall)
	if before := ansi.Strip(app.View().Content); !strings.Contains(before, stopsTheTurn) {
		t.Fatalf("the footer before the press does not offer to stop the turn\n%s", before)
	}
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	after := ansi.Strip(app.View().Content)
	if !strings.Contains(after, stopsTheTools) {
		t.Fatalf("the footer during the soft stop does not say what a second ctrl+c does\n%s", after)
	}
	if strings.Contains(after, stopsTheTurn) {
		t.Fatalf("the footer still offers a press that no longer stops the turn\n%s", after)
	}
}

func TestAnInterruptKeepsWhatWasTypedWhileTheTurnRan(t *testing.T) {
	app, _ := callingApp(t, shellCall)
	typeAndSend(app, firstTask)
	typeAndSend(app, secondTask)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if queued := app.view.Queued(); len(queued) != 2 {
		t.Fatalf("ctrl+c left %q in the queue, want both messages", queued)
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, "2 messages"+queuedTyped) {
		t.Fatalf("the screen does not say what the queue keeps\n%s", plain)
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
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: narrowColumns, Height: narrowRows})
	for step := range longSteps {
		app.Update(Event{Kind: EventNote, Text: "toolgate.go step " + strconv.Itoa(step)})
	}
	return app
}

func transcriptOf(frame string) string {
	above, _, _ := strings.Cut(frame, composerTint())
	return ansi.Strip(above)
}

func TestALongTranscriptScrollsAWheelAScreenToTheStartAndBackToTheTail(t *testing.T) {
	app := longApp(t)
	tail := app.View().Content
	golden.Assert(t, "scroll-tail-80x24.golden", tail)

	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	golden.Assert(t, "scroll-line-80x24.golden", app.View().Content)

	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	golden.Assert(t, "scroll-screen-80x24.golden", app.View().Content)

	app.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	start := app.View().Content
	golden.Assert(t, "scroll-start-80x24.golden", start)
	if !strings.Contains(transcriptOf(start), "toolgate.go step 0") {
		t.Fatalf("home did not reach the first entry\n%s", start)
	}
	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
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
	before2Tools := strings.Count(ansi.Strip(before), ") tools")
	app.Update(Event{Kind: EventToolCall, Tool: "bash", Text: "arrived while scrolled back"})
	after := app.View().Content
	if after != before {
		t.Fatalf("new output moved the view\n--- after ---\n%s\n--- before ---\n%s", after, before)
	}
	if strings.Count(ansi.Strip(after), ") tools") != before2Tools {
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
	if !strings.Contains(ansi.Strip(after), ") tools") {
		t.Fatalf("the following view did not draw the new entry\n%s", after)
	}
}

func TestAShortTranscriptCannotBeScrolledAndSaysNothingAboutIt(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
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
	golden.Assert(t, "session-one-wire-80x24.golden", before)
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
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Turn:   func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {},
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
		"the heading":    "Tofu",
		"the code span":  " toolgate.go ",
		"the fenced go":  "func Decide(answers Answers) Verdict",
		"the list":       "• a question is asked once",
		"the table rule": "─┼─",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("%s is missing, want %q in\n%s", what, want, plain)
		}
	}
	if strings.Contains(plain, "**gate**") || strings.Contains(plain, "`toolgate.go`") || strings.Contains(plain, "## Tofu") {
		t.Errorf("the markdown is shown raw\n%s", plain)
	}
	if !strings.Contains(content, "\x1b[38;5;255;1mgate\x1b[m") {
		t.Errorf("the bold span is not bold\n%q", content)
	}
	golden.Assert(t, "session-markdown-80x40.golden", content)
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
	golden.Assert(t, "session-mermaid-80x24.golden", content)
}

func TestAStreamingMessageRendersMarkdownAndAClosedOneNeedsNoReveal(t *testing.T) {
	app := proseApp(t, 24)
	app.Update(Event{Kind: EventTextDelta, Text: "The **gate** reads "})
	app.Update(Event{Kind: EventTextDelta, Text: "`toolgate.go` before the policy.\n"})
	streaming := app.View().Content
	plain := ansi.Strip(streaming)
	if strings.Contains(plain, "**gate**") || strings.Contains(plain, "`toolgate.go`") {
		t.Errorf("a streaming message still carries raw markdown\n%s", plain)
	}
	if !strings.Contains(plain, "gate") || !strings.Contains(plain, "toolgate.go") {
		t.Errorf("the streaming text is missing\n%s", plain)
	}
	golden.Assert(t, "session-streaming-80x24.golden", streaming)

	app.Update(Closed{})
	complete := app.View().Content
	if complete != streaming {
		t.Fatalf("a message with nothing left open changed once it stopped\n--- streaming ---\n%s\n--- complete ---\n%s", plain, ansi.Strip(complete))
	}
	golden.Assert(t, "session-complete-80x24.golden", complete)
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
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	subAgentFrame := app.View().Content
	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if app.View().Content != subAgentFrame {
		t.Fatal("the wheel scrolled while another view was open")
	}
}

func TestTheSetupScreenDrawsNoStatusBar(t *testing.T) {
	setup := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Requirements: setupRequirements()})
	setup.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	running := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: anthropicAlone})
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

func zero() *int { code := 0; return &code }

func shellEntries() []shells.Entry {
	return []shells.Entry{
		{Name: "dev-server", Command: "npm run dev", State: shells.Running, Started: time.Date(2026, 9, 19, 14, 30, 0, 0, time.UTC), Log: "listening on :3000"},
		{Name: "build", Command: "go build ./...", State: shells.Exited, Started: time.Date(2026, 9, 19, 14, 31, 0, 0, time.UTC), ExitCode: zero(), Log: "compiling\ndone"},
		{Name: "test-run", Command: "go test ./...", State: shells.Exited, Started: time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC), ExitCode: zero(), Log: "ok tofu/internal/shell 0.4s"},
	}
}

func shellsApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := sessionApp(t, width, height)
	app.Update(shellsMsg(shellEntries()))
	app.Update(tea.KeyPressMsg{Code: '5', Mod: tea.ModAlt})
	if app.current != viewShells {
		t.Fatalf("alt+5 left the app on view %d, want shells", app.current)
	}
	return app
}

func TestTheShellsViewListsNamedProcessesWithStateAndRecentOutput(t *testing.T) {
	content := shellsApp(t, 120, 36).View().Content
	for _, want := range []string{"dev-server", "npm run dev", "build", "go build ./...", "test-run", "listening on :3000"} {
		if !strings.Contains(ansi.Strip(content), want) {
			t.Errorf("the shells view does not show %q\n%s", want, ansi.Strip(content))
		}
	}
	golden.Assert(t, "shells-120x36.golden", content)
}

func TestTheEmptyShellsViewSaysNoProcessIsRunning(t *testing.T) {
	app := sessionApp(t, 80, 24)
	app.Update(tea.KeyPressMsg{Code: '5', Mod: tea.ModAlt})
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "no process is running") {
		t.Fatalf("the empty shells view does not say there is nothing to see\n%s", content)
	}
	golden.Assert(t, "shells-empty-80x24.golden", app.View().Content)
}

func TestTheShellsViewMovesOnJAndKAndKillsOnCtrlX(t *testing.T) {
	killed := ""
	app := sessionApp(t, 120, 36)
	app.options.KillShell = func(name string) error { killed = name; return nil }
	app.Update(shellsMsg(shellEntries()))
	app.Update(tea.KeyPressMsg{Code: '5', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: 'j'})
	app.Update(tea.KeyPressMsg{Code: 'k'})
	if killed != "" {
		t.Fatalf("a letter that moves the list killed %q", killed)
	}
	app.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if killed != "dev-server" {
		t.Fatalf("killing the picked process called KillShell with %q, want dev-server", killed)
	}
	content := ansi.Strip(app.View().Content)
	if strings.Contains(content, "dev-server") {
		t.Errorf("a killed process is still in the view\n%s", content)
	}
	if !strings.Contains(content, "build") {
		t.Errorf("killing one process dropped an unrelated one\n%s", content)
	}
}

func TestNeitherFileEditsNorShellsTakeChatInput(t *testing.T) {
	app := sessionApp(t, 80, 24)
	edited(app, "e1", "", gatePath, gateDiff)
	app.Update(tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt})
	before := app.View().Content
	app.Update(tea.KeyPressMsg{Text: "x"})
	if app.View().Content != before {
		t.Errorf("a letter typed in the file edits view changed what is drawn")
	}

	app.Update(shellsMsg(shellEntries()))
	app.Update(tea.KeyPressMsg{Code: '5', Mod: tea.ModAlt})
	before = app.View().Content
	app.Update(tea.KeyPressMsg{Text: "x"})
	if app.View().Content != before {
		t.Errorf("a letter typed in the shells view changed what is drawn")
	}
}

const halfWritten = "the loop reads the policy first, then the wire, because a locked"

func interruptedAnswer(t *testing.T, app *App) work.Entry {
	t.Helper()
	for _, entry := range app.work.Entries {
		if entry.Head == partialHead {
			return entry
		}
	}
	t.Fatalf("work holds no interrupted answer\n%s", ansi.Strip(app.View().Content))
	return work.Entry{}
}

func TestASoftStopBeforeTheHardStopKeepsTheSameAnswerInWork(t *testing.T) {
	hard, hardStopped := turningApp(t)
	hard.Update(Event{Kind: EventTextDelta, Text: halfWritten})
	interrupt(hard, 1)
	<-hardStopped

	soft, softStopped := turningApp(t)
	soft.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "go test ./..."})
	soft.Update(Event{Kind: EventTextDelta, Text: halfWritten})
	interrupt(soft, 2)
	<-softStopped

	if held := interruptedAnswer(t, soft).Output; held != interruptedAnswer(t, hard).Output {
		t.Errorf("two stops left %q in work, one stop left the half written answer", held)
	}
}

func TestCookedForNamesTheAnswerWhenAToolCallCameAfterIt(t *testing.T) {
	app, stopped := turningApp(t)
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "go test ./..."})
	app.Update(Event{Kind: EventTextDelta, Text: halfWritten})
	interrupt(app, 1)
	app.Update(Event{Kind: EventToolCall, ID: "c2", Tool: "read", Text: "internal/turn/loop.go"})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "ok"})
	app.Update(Event{Kind: EventToolResult, ID: "c2", Text: "84 lines"})
	<-stopped
	app.Update(Event{Kind: EventDone, Text: "cooked for"})
	app.Update(Closed{})

	want := "cooked for 0s · [" + trace.Short(interruptedAnswer(t, app).ID) + "]"
	if chat := ansi.Strip(app.View().Content); !strings.Contains(chat, want) {
		t.Errorf("no line reads %q\n%s", want, chat)
	}
}

func TestAStoppedTurnKeepsItsToolRowsWhileToolsAreFoldedByDefault(t *testing.T) {
	app, stopped := turningApp(t)
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "printf HALFWAY"})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "HALFWAY", Bytes: 7})
	interrupt(app, 1)
	<-stopped
	app.Update(Closed{})

	chat := ansi.Strip(app.View().Content)
	for _, line := range strings.Split(chat, "\n") {
		if strings.Contains(line, "⟩ bash printf HALFWAY") && strings.Count(line, "HALFWAY") == 2 {
			return
		}
	}
	t.Fatalf("the stopped turn folded away the call and what it printed\n%s", chat)
}
