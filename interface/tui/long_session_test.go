package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/internal/konst"
	roster "tofu/internal/subagent"
)

const (
	longAgents      = 117
	longCalls       = 4700
	longBusiestCall = 210
	longEditEvery   = 8
	longShells      = 40
	longShellLines  = 200
	longFrames      = 9
	longWidth       = 160
	longHeight      = 45
)

type longSession struct {
	app  *App
	rows []subagent.Row
	at   *time.Time
}

func longEdit(path string, index int) string {
	return "--- " + path + "\n+++ " + path + "\n@@ -1,4 +1,5 @@\n func f" + strconv.Itoa(index) + "() {\n-\treturn old(x)\n+\treturn next(x)\n+\treturn next(y)\n }\n"
}

func longShellEntries(at time.Time) []shells.Entry {
	entries := make([]shells.Entry, longShells)
	for index := range entries {
		var log strings.Builder
		for line := range longShellLines {
			log.WriteString("line " + strconv.Itoa(line) + " of the dev server log\n")
		}
		state := shells.Exited
		if index%5 == 0 {
			state = shells.Running
		}
		entries[index] = shells.Entry{Name: "shell-" + strconv.Itoa(index), Command: "npm run dev", State: state, Started: at, PID: 1000 + index,
			Dir: "/repo", Owner: "go-dev-" + strconv.Itoa(index), Log: log.String()}
	}
	return entries
}

func generatedLongSession(t *testing.T) longSession {
	t.Helper()
	at := fixedStart()
	app := phaseApp(t, &at)
	shellEntries := longShellEntries(at)
	app.options.Shells = func() []shells.Entry { return shellEntries }
	app.Update(tea.WindowSizeMsg{Width: longWidth, Height: longHeight})
	rows := make([]subagent.Row, longAgents)
	for index := range rows {
		name := "go-dev-" + strconv.Itoa(index)
		rows[index] = subagent.Row{Name: name, Agent: "go-dev", Model: "claude-sub/claude-opus-5", Owns: []string{"internal/pkg" + strconv.Itoa(index) + "/**"},
			Doing: "the work of " + name, Total: konst.TurnMaxSteps, State: roster.Finished, Report: "done with " + name}
		app.Update(Event{Kind: EventToolCall, ID: "spawn-" + name, Tool: "spawn", Text: rows[index].Doing, Promote: true})
		app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(rows[:index+1])})
		app.Update(Event{Kind: EventToolResult, ID: "spawn-" + name, Text: "spawned"})
	}
	for call := range longCalls {
		agent := call % longAgents
		if call < longBusiestCall {
			agent = 0
		}
		name, id := rows[agent].Name, "call-"+strconv.Itoa(call)
		at = at.Add(time.Second)
		app.Update(Event{Kind: EventThinking, ID: "think-" + strconv.Itoa(call), Agent: name, Text: "reading the package before the change"})
		path := "internal/pkg" + strconv.Itoa(agent) + "/file" + strconv.Itoa(call%7) + ".go"
		app.Update(Event{Kind: EventToolCall, ID: id, Agent: name, Tool: "read", Text: path})
		result := Event{Kind: EventToolResult, ID: id, Agent: name, Text: "84 lines, 2.1 KB"}
		if call%longEditEvery == 0 {
			result.Diff = longEdit(path, call)
		}
		app.Update(result)
		rows[agent].Calls = append(rows[agent].Calls, subagent.Call{ID: id, At: at, Tool: "read", Text: path, Result: "84 lines"})
		rows[agent].Steps++
	}
	app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(rows)})
	app.Update(shellsMsg(shellEntries))
	app.View()
	return longSession{app: app, rows: rows, at: &at}
}

func (s longSession) frames(t *testing.T, label string, step func(at int)) {
	t.Helper()
	taken := make([]time.Duration, longFrames)
	for at := range taken {
		began := time.Now()
		step(at)
		s.app.View()
		taken[at] = time.Since(began)
	}
	slices.Sort(taken)
	median, worst := taken[len(taken)/2], taken[len(taken)-1]
	t.Logf("%-44s median %7.2f ms  worst %7.2f ms  budget %.1f ms", label, ms(median), ms(worst), float64(konst.FrameBudgetMicros)/1000)
	if median > konst.FrameBudgetMicros*time.Microsecond {
		t.Errorf("%s: median %v over the %v budget", label, median, konst.FrameBudgetMicros*time.Microsecond)
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func (s longSession) press(keys ...string) func(int) {
	return func(at int) {
		s.app.Update(keyPress(keys[at%len(keys)]))
	}
}

func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Code: rune(name[0]), Text: name}
}

func (s longSession) working(at int) {
	busy := &s.rows[1+at%(longAgents-1)]
	busy.State = roster.Working
	*s.at = s.at.Add(time.Second)
	id := "live-" + strconv.Itoa(at)
	busy.Calls = append(busy.Calls, subagent.Call{ID: id, At: *s.at, Tool: "bash", Text: "go test ./..."})
	s.app.Update(Event{Kind: EventToolCall, ID: id, Agent: busy.Name, Tool: "bash", Text: "go test ./..."})
	s.app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(s.rows)})
	s.app.Update(pulseMsg{})
}

func TestLongSessionFramesStayInsideTheBudget(t *testing.T) {
	s := generatedLongSession(t)
	if got := len(s.app.happened); got < 9000 {
		t.Fatalf("the generated session holds %d activity events, want the 9,389 shape", got)
	}
	s.app.show(screenAgents)
	s.frames(t, "sub-agents, steady", func(int) {})
	s.frames(t, "sub-agents, pgup/pgdown at the newest", s.press("right", "pgup", "pgdown"))
	s.app.feed.SetScroll(1 << 30)
	s.frames(t, "sub-agents, pgdown/pgup at the oldest", s.press("pgdown", "pgup"))
	s.frames(t, "sub-agents, switch agent on the rail", s.press("left", "j", "j", "k"))
	s.app.feed.SelectAgent(s.rows[0].Name, 0)
	s.app.feed.SetScroll(1 << 30)
	s.frames(t, "sub-agents, agent detail at its oldest", s.press("pgdown", "pgup"))
	s.frames(t, "sub-agents, agents working", s.working)
	s.app.show(screenEdits)
	s.frames(t, "file edits, next and previous edit", s.press("n", "p"))
	s.frames(t, "file edits, pgup/pgdown", s.press("pgup", "pgdown"))
	s.frames(t, "file edits, agents working", s.working)
	s.app.show(screenShells)
	s.frames(t, "shells, down/up", s.press("j", "up"))
	s.frames(t, "shells, pgup/pgdown", s.press("pgup", "pgdown"))
	s.frames(t, "shells, agents working", s.working)
}
