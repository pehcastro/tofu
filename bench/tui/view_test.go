package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "tofu/interface/tui"
	"tofu/interface/tui/fixture"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/subagent"
	"tofu/internal/konst"
	isettings "tofu/internal/settings"
)

const (
	benchWidth          = 120
	benchHeight         = 36
	benchTranscript     = 200
	benchLongTranscript = 10000
	benchSubAgents      = 6
	benchSubAgentCalls  = 60
)

func benchApp(b *testing.B) *app.App {
	b.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	store, err := isettings.Open(filepath.Join(b.TempDir(), "settings.json"), "")
	if err != nil {
		b.Fatal(err)
	}
	if err := store.Set(isettings.Global, isettings.ChatShowsTools, 1); err != nil {
		b.Fatal(err)
	}
	built := app.New(app.Options{
		Repo:     "silo",
		Branch:   "develop",
		Release:  "test",
		Settings: store,
		Keymap:   filepath.Join(b.TempDir(), "shortcuts.json"),
		Wires:    func() []app.Wire { return []app.Wire{{Name: "anthropic", Model: "claude-opus-5"}} },
		Paths:    benchRepoPaths,
		Now:      func() time.Time { return at },
		Turn:     func(context.Context, app.Pick, string, app.CalledFromInsideTheTurnAndNeverAfterItReturns) {},
		Providers: []settings.Provider{
			{Name: "anthropic", State: "oauth  62% of the 7d window, resets 18:00", Source: "the credential store"},
			{Name: "openrouter", Key: "sk-or-v1-77c1f0b6e5a94d2f8badc0ffee1234567890abcd", State: "ok", Source: ".env at ~/.tofu/.env"},
			{Name: "jev", State: "build jev-2026-09-01", Source: "the last decision"},
			{Name: "codex", Fix: "tofu login codex", Source: "nothing is stored"},
		},
	})
	built.Init()
	built.Update(tea.WindowSizeMsg{Width: benchWidth, Height: benchHeight})
	built.Update(app.Event{Kind: app.EventContext, Context: fixture.Context()})
	built.Update(app.Event{Kind: app.EventForkStart})
	for step := range benchTranscript {
		call := "c" + strconv.Itoa(step)
		built.Update(app.Event{Kind: app.EventToolCall, ID: call, Tool: "read",
			Text: "internal/judge/policy/toolgate.go line " + strconv.Itoa(step)})
		built.Update(app.Event{Kind: app.EventToolResult, ID: call, Text: "412 lines, 11.8 KB"})
	}
	return built
}

func BenchmarkSessionView(b *testing.B) {
	built := benchApp(b)
	if built.View().Cursor == nil {
		b.Fatal("the bench is not measuring a frame that places the cursor")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

const benchMessages = 20

const benchAnswer = "## the gate\n\nThe **gate** reads `toolgate.go` before the policy.\n\n" +
	"```go\nfunc Decide(answers Answers) Verdict\n```\n\n" +
	"- a question is asked once\n- a verdict is always logged\n\n" +
	"| point | verdict |\n| --- | --- |\n| tool_gate | ask |\n"

func BenchmarkSessionViewWithProse(b *testing.B) {
	built := benchApp(b)
	for range benchMessages {
		built.Update(app.Event{Kind: app.EventText, Text: benchAnswer})
	}
	if !strings.Contains(built.View().Content, "toolgate.go") {
		b.Fatal("the bench is not measuring a transcript of rendered messages")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func benchLongApp(b *testing.B) *app.App {
	b.Helper()
	built := benchApp(b)
	for step := range benchLongTranscript {
		built.Update(app.Event{Kind: app.EventToolCall, ID: "long" + strconv.Itoa(step), Tool: "read",
			Text: "toolgate.go line " + strconv.Itoa(step)})
	}
	built.Update(openKey())
	return built
}

func openKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl} }

func BenchmarkSessionViewLongTranscriptAtTheTail(b *testing.B) {
	built := benchLongApp(b)
	if !strings.Contains(built.View().Content, "line "+strconv.Itoa(benchLongTranscript-1)) {
		b.Fatal("the bench is not measuring the tail of the long transcript")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkSessionViewLongTranscriptScrolledBack(b *testing.B) {
	built := benchLongApp(b)
	bar, _ := trackOf(built)
	pressTrack(built, bar.top)
	if strings.Contains(built.View().Content, "line "+strconv.Itoa(benchLongTranscript-1)) {
		b.Fatal("the bench is not measuring a scrolled transcript")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkSessionScrollLongTranscript(b *testing.B) {
	built := benchLongApp(b)
	b.ReportAllocs()
	for b.Loop() {
		built.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		built.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
}

const (
	benchPastes    = 3
	benchPastedFmt = "turn-19a2b3c4d5-image-0%d.png"
	benchPastedLen = 1_482_304
)

func BenchmarkSessionViewWithPastedImages(b *testing.B) {
	built := benchApp(b)
	for image := range benchPastes {
		built.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
		built.Update(paste.Outcome{
			Index: image + 1,
			State: paste.Ready,
			Name:  fmt.Sprintf(benchPastedFmt, image+1),
			Bytes: benchPastedLen,
		})
	}
	if !strings.Contains(built.View().Content, "[Image #"+strconv.Itoa(benchPastes)+"]") {
		b.Fatal("the bench is not measuring a frame holding three pasted images")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func benchDecision(verdict session.Verdict) *session.Decision {
	return &session.Decision{
		Tool:    "read",
		Verdict: verdict,
		Answers: []session.Answer{
			{Question: "approval", Value: 0.75, Max: 1},
			{Question: "from_untrusted", Value: 0.02, Max: 1},
			{Question: "risk", Value: 2, Max: 3},
			{Question: "user_requested", Value: 0.11, Max: 1},
		},
		Reason: session.Reason{Question: "risk", Limit: "risk_ask_at", Threshold: 1.5, Value: 2},
	}
}

func BenchmarkSessionViewWithDecisions(b *testing.B) {
	built := benchApp(b)
	for step := range benchTranscript {
		verdict := session.Allow
		if step%2 == 1 {
			verdict = session.Ask
		}
		built.Update(app.Event{Kind: app.EventDecision, Decision: benchDecision(verdict)})
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func benchSend(built *app.App, task string) {
	for _, letter := range task {
		built.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	built.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

const benchQueued = 3

func BenchmarkSessionViewWithThreeQueuedMessages(b *testing.B) {
	built := benchApp(b)
	benchSend(built, "why does the gate read the policy first?")
	for step := range benchQueued {
		benchSend(built, "and then read internal/point/toolgate"+strconv.Itoa(step)+".go")
	}
	if !strings.Contains(built.View().Content, "waiting") {
		b.Fatal("the bench is not measuring a frame carrying a queue")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkSessionViewAwaitingAnAnswer(b *testing.B) {
	built := benchApp(b)
	benchSend(built, "push the branch")
	built.Update(app.Event{Kind: app.EventToolCall, ID: "ask", Tool: "read", Text: "internal/judge/policy/toolgate.go"})
	built.Update(app.Event{Kind: app.EventDecision, Decision: benchDecision(session.Ask)})
	built.Update(app.Event{Kind: app.EventAwaitPerson})
	if !strings.Contains(built.View().Content, "always here") {
		b.Fatal("the bench is not measuring a frame that waits on an answer")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func benchSubAgentRows() []subagent.Row {
	rows := make([]subagent.Row, 0, benchSubAgents)
	for index := range benchSubAgents {
		row := subagent.Row{
			Name:   "go-dev-" + strconv.Itoa(index),
			Owns:   []string{"internal/judge/**", "internal/point/" + strconv.Itoa(index) + "/**"},
			Doing:  "writing internal/judge/policy/toolgate.go",
			Since:  time.Duration(index) * time.Minute,
			Steps:  index,
			Total:  benchSubAgents,
			State:  subagent.State(index % 3),
			Tokens: 181000 - index*1000,
			Report: "renamed the interface and its five implementations, and one call site still reaches the old name through an alias.",
		}
		for step := range benchSubAgentCalls {
			row.Calls = append(row.Calls, subagent.Call{
				Tool:   "edit",
				Text:   "internal/judge/policy/toolgate.go line " + strconv.Itoa(step),
				Result: "+18 -4",
			})
		}
		rows = append(rows, row)
	}
	return rows
}

func BenchmarkSessionViewWithTheActivityBlock(b *testing.B) {
	built := benchApp(b)
	built.Update(app.Event{Kind: app.EventSubAgent, SubAgents: benchSubAgentRows()})
	built.Update(app.Event{Kind: app.EventToolCall, ID: "live", Tool: "bash", Text: "go test ./internal/..."})
	if !strings.Contains(built.View().Content, "go-dev-0") {
		b.Fatal("the bench is not measuring a frame carrying the activity block")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func benchPlan() []session.PlanItem {
	return []session.PlanItem{
		{Phase: "read", Text: "read the gate and the record shape", State: session.PlanDone},
		{Phase: "write", Text: "write the plan tool", State: session.PlanRunning},
		{Phase: "write", Text: "draw the plan in the session view", State: session.PlanPending},
		{Phase: "check", Text: "measure the frame against the budget", State: session.PlanPending},
	}
}

func BenchmarkSessionViewWithAPlan(b *testing.B) {
	built := benchApp(b)
	built.Update(app.Event{Kind: app.EventPlan, Plan: benchPlan()})
	if !strings.Contains(built.View().Content, "write the plan tool") {
		b.Fatal("the bench is not measuring a frame carrying the plan")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkSessionViewWithTheCommandMenuOpen(b *testing.B) {
	built := benchApp(b)
	for _, letter := range "/se" {
		built.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	if !strings.Contains(built.View().Content, "/settings") {
		b.Fatal("the bench is not measuring a frame carrying the command menu")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

const benchPaths = 5000

func benchRepoPaths() []string {
	paths := make([]string, 0, benchPaths)
	for index := range benchPaths {
		paths = append(paths, "internal/judge/policy/toolgate"+strconv.Itoa(index)+".go")
	}
	return paths
}

func BenchmarkSessionViewWithThePathMenuOpen(b *testing.B) {
	built := benchApp(b)
	built.Update(built.Init()())
	for _, letter := range "read @toolgate" + strconv.Itoa(benchPaths-1) {
		built.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	if !strings.Contains(built.View().Content, "Reference a file") {
		b.Fatal("the bench is not measuring a frame carrying the path menu")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkSubAgentView(b *testing.B) {
	built := benchApp(b)
	built.Update(app.Event{Kind: app.EventSubAgent, SubAgents: benchSubAgentRows()})
	onScreen(built, "sub-agents", sitting{})
	built.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if !strings.Contains(built.View().Content, "go-dev-0") {
		b.Fatal("the bench is not measuring the sub-agent view")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

const benchEditDiff = "--- internal/judge/policy/toolgate.go\n" +
	"+++ internal/judge/policy/toolgate.go\n" +
	"@@ -40,6 +40,7 @@\n" +
	" func Decide(answers Answers) Verdict {\n" +
	"\tif answers.Risk > thresholds.RiskAskAt {\n" +
	"-\t\treturn Ask\n" +
	"+\t\treturn AskWithReason(answers)\n" +
	"\t}\n" +
	"\treturn Allow\n" +
	"}\n"

func benchEditsApp(b *testing.B) *app.App {
	b.Helper()
	built := benchApp(b)
	built.Update(app.Event{Kind: app.EventSubAgent, SubAgents: benchSubAgentRows()})
	for step := range benchTranscript {
		id := "edit" + strconv.Itoa(step)
		path := "internal/judge/policy/toolgate" + strconv.Itoa(step) + ".go"
		built.Update(app.Event{Kind: app.EventToolCall, ID: id, Tool: "edit", Text: path})
		built.Update(app.Event{Kind: app.EventToolResult, ID: id, Text: "9 lines, 210 bytes",
			Agent: benchSubAgentRows()[step%benchSubAgents].Name, Diff: benchEditDiff})
	}
	key(built, tea.KeyPressMsg{Code: tea.KeyTab}, 2)
	return built
}

func BenchmarkFileEditsView(b *testing.B) {
	built := benchEditsApp(b)
	key(built, tea.KeyPressMsg{Code: 'n', Text: "n"}, 1)
	if !strings.Contains(built.View().Content, "AskWithReason") {
		b.Fatal("the bench is not measuring a file edits view holding diffs")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

const benchCreatedLines = 500

func BenchmarkFileEditsViewWithACreatedFile(b *testing.B) {
	built := benchEditsApp(b)
	var content strings.Builder
	for line := range benchCreatedLines {
		content.WriteString("\tcase konst.Limit" + strconv.Itoa(line) + ": return konst.Limit" + strconv.Itoa(line) + "\n")
	}
	built.Update(app.Event{Kind: app.EventToolCall, ID: "created", Tool: "write", Text: "internal/konst/konst.go"})
	built.Update(app.Event{Kind: app.EventToolResult, ID: "created", Text: "created internal/konst/konst.go",
		Created: content.String()})
	if !strings.Contains(built.View().Content, "+500") {
		b.Fatal("the bench is not measuring a file edits view holding a created file")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkFileEditsViewFilteredToOneAgent(b *testing.B) {
	built := benchEditsApp(b)
	built.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	built.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func BenchmarkTopAndFooter(b *testing.B) {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	head := frame.Head{Path: "silo", Branch: "develop", Provider: "claude-sub", Model: "claude-opus-5", SessionName: "keen-brass-mole", SessionID: "turn-18d724d865ed9264", At: at, Started: at.Add(-time.Hour)}
	tabs := []frame.Tab{{Label: "chat"}, {Label: "sub-agents", Count: 2}, {Label: "file edits"}, {Label: "shells"}}
	status := frame.Status{Context: frame.Context{Used: 118000, Budget: konst.ContextCeilingTokens}, At: at}
	right := frame.ChatRight("claude-sub/claude-opus-5", "high")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = frame.Top(head, tabs, 0, benchWidth)
		_ = frame.Footer(status, benchWidth, right)
	}
}

func BenchmarkSettingsView(b *testing.B) {
	built := benchApp(b)
	onScreen(built, "settings", sitting{})
	b.ReportAllocs()
	for b.Loop() {
		_ = built.View()
	}
}

func setupApp(t *testing.T, width, height int) *app.App {
	t.Helper()
	required := []app.Requirement{
		{What: "no subscription is signed in, so no model can answer",
			Fix: "tofu login anthropic, which opens the browser; tofu login codex signs in the other subscription"},
		{What: "there is no openrouter key, so jev judges no tool call",
			Fix: "tofu login openrouter, which asks for the key and checks it reaches jev"},
	}
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	built := app.New(app.Options{
		Repo:         "silo",
		Branch:       "develop",
		Release:      "test",
		Now:          func() time.Time { return at },
		Requirements: required,
		Recheck:      func() []app.Requirement { return required },
	})
	built.Init()
	built.Update(tea.WindowSizeMsg{Width: width, Height: height})
	if !strings.Contains(built.View().Content, "openrouter") {
		t.Fatal("this is not the setup screen")
	}
	return built
}

const (
	frameBudget = konst.FrameBudgetMicros * time.Microsecond
	framesTimed = 100
)

func TestTheSetupViewDrawsInsideTheFrameBudget(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 36}} {
		built := setupApp(t, size.width, size.height)
		for round := range konst.FrameBudgetAttempts {
			began := time.Now()
			for range framesTimed {
				_ = built.View()
			}
			each := time.Since(began) / framesTimed
			t.Logf("%dx%d round %d: %v a frame over %d frames", size.width, size.height, round+1, each, framesTimed)
			if each > frameBudget {
				t.Errorf("the setup view at %dx%d takes %v a frame, over the %v budget", size.width, size.height, each, frameBudget)
			}
		}
	}
}
