package filmstrip

import (
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	roster "tofu/internal/subagent"
)

const (
	createdLines   = 1200
	servedRequests = 40
	sittingPose    = "sitting"
	lyingPose      = "lying"
	narrowWidth    = 60
	narrowHeight   = 20
	welcomeDraft   = "why does the gate read the policy first?"
	resolveGoPath  = "internal/judge/policy/resolve.go"
)

func done() []tui.Event {
	return []tui.Event{{Kind: tui.EventDone, Text: "cooked for"}}
}

func agentWork(at time.Time) []tui.Event {
	subAgents := []subagent.Row{
		{Name: "c1", Doing: "read the policy loader", Owns: []string{"internal/judge/policy/**"}, Since: 94 * time.Second, Steps: 3, Total: 40, Tokens: 18200, State: roster.Working,
			Calls: []subagent.Call{
				{ID: "7a1c02", At: at, Tool: "read", Text: resolveGoPath, Result: "209 lines, 6.2 KB"},
				{ID: "7a1c03", At: at, Tool: "grep", Text: "Resolve( internal/judge"},
			}},
		{Name: "c2", Doing: "write the ledger floor test", Owns: []string{"internal/judge/ledger/**"}, Since: 3 * time.Minute, Steps: 7, Total: 40, Tokens: 41800, State: roster.InReview,
			Calls:  []subagent.Call{{ID: "7b2d01", At: at, Tool: "edit", Text: "internal/judge/ledger/floor.go", Result: "+14 -2"}},
			Report: "the floor test fails on an empty ledger and passes on one row"},
		{Name: "c3", Doing: "run the shell suite", Owns: []string{"internal/shell/**"}, Since: 51 * time.Second, Steps: 2, Total: 40, Tokens: 6100, State: roster.Errored,
			Calls:  []subagent.Call{{ID: "7c3e01", At: at, Tool: "bash", Text: "go test ./internal/shell/...", Result: "exit 1\n--- FAIL: TestKillTree (0.31s)"}},
			Report: "exit 1: TestKillTree could not find the job object"},
	}
	events := []tui.Event{{Kind: tui.EventRequesting}}
	for index, subAgent := range subAgents {
		id := "5e0" + strconv.Itoa(index+1) + "aa"
		events = append(events, tui.Event{Kind: tui.EventToolCall, ID: id, Tool: "spawn", Text: subAgent.Doing, Promote: true},
			tui.Event{Kind: tui.EventSubAgent, SubAgents: subAgents[:index+1]},
			result(id, "spawned [&"+subAgent.Name+"]"))
	}
	events = append(events,
		call("9d4f10", "read", "internal/turn/loop.go"),
		result("9d4f10", "412 lines, 11.8 KB"),
		tui.Event{Kind: tui.EventText, ID: "9d4f11", Text: "three sub-agents run: [&c1] reads the loader, [&c2] waits on review, [&c3] failed its suite. [tool#9d4f10] is the loop I read."})
	return append(events, done()...)
}

func modifiedDiff() string {
	return strings.Join([]string{
		"--- a/" + resolveGoPath,
		"+++ b/" + resolveGoPath,
		"@@ -12,9 +12,11 @@ func Resolve(declared Policy, lock Lock) (Resolved, error) {",
		" 	if lock.Empty() {",
		" 		return Resolved{Mode: Shadow}, nil",
		" 	}",
		"-	if declared.Mode == Enforced {",
		"-		return Resolved{Mode: Enforced}, nil",
		"+	if declared.Mode == Enforced && lock.Valid(declared) {",
		"+		return Resolved{Mode: Enforced, Lock: lock.ID}, nil",
		" 	}",
		"+	if declared.Mode == Enforced {",
		"+		return Resolved{Mode: Shadow, Why: \"the lock does not match\"}, nil",
		"+	}",
		" 	return Resolved{Mode: declared.Mode}, nil",
		" }",
	}, "\n")
}

func createdFile() string {
	rows := make([]string, createdLines)
	rows[0] = "package ledger"
	for index := 1; index < createdLines; index++ {
		rows[index] = "var row" + strconv.Itoa(index+1) + " = floorRow{Question: \"risk\", Line: " + strconv.Itoa(index+1) + "}"
	}
	rows[createdLines-1] = "var lastRow = floorRow{Question: \"risk\", Line: " + strconv.Itoa(createdLines) + "}"
	return strings.Join(rows, "\n")
}

func fileWork() []tui.Event {
	return append([]tui.Event{
		call("e5a101", "edit", resolveGoPath),
		{Kind: tui.EventToolResult, ID: "e5a101", Text: "ok", Diff: modifiedDiff()},
		call("e5a102", "write", "internal/judge/ledger/floor_rows.go"),
		{Kind: tui.EventToolResult, ID: "e5a102", Text: "ok", Created: createdFile()},
		{Kind: tui.EventText, ID: "e5a103", Text: "the resolver checks the lock now, and [edit#e5a102] holds the 1200 floor rows."},
	}, done()...)
}

func shellEntries(at time.Time) []shells.Entry {
	exited, code := at.Add(-40*time.Second), 0
	served := []string{"listening on 127.0.0.1:7411"}
	for request := 1; request <= servedRequests; request++ {
		served = append(served, "GET /ledger/"+strconv.Itoa(request)+" 200 "+strconv.Itoa(request%7+1)+".1ms")
	}
	served = append(served, "warning: slow request /ledger/floor 812ms")
	return []shells.Entry{
		{Name: "dev-server", Command: "go run ./cmd/tofu serve --port 7411", State: shells.Running, Started: at.Add(-7 * time.Minute), PID: 18244,
			Dir: "internal/serve", Owner: "c1", Log: strings.Join(served, "\n")},
		{Name: "suite", Command: "go test ./internal/judge/...", State: shells.Exited, Started: at.Add(-2 * time.Minute), Ended: &exited, ExitCode: &code, PID: 17902,
			Dir: ".", Owner: "orchestrator", Log: "ok  tofu/internal/judge 0.42s\nok  tofu/internal/judge/policy 0.18s\nok  tofu/internal/judge/ledger 0.09s"},
	}
}

func withShells(options *tui.Options) {
	at := options.Now()
	options.Shells = func() []shells.Entry { return shellEntries(at) }
	options.KillShell = func(string) error { return nil }
}

func keys(names ...string) func(*reel) { return func(r *reel) { r.press(names...) } }

func openSettings(r *reel) {
	r.driver.Type("/settings")
	r.press("enter")
}

func screens() []scenario {
	var categories []beat
	for index, name := range []string{"appearance", "models-and-roles", "interaction", "context", "files", "shell", "turn", "browser"} {
		step := func(r *reel) { r.press("right") }
		if index == 0 {
			step = openSettings
		}
		categories = append(categories, beat{name, step})
	}
	return []scenario{
		{name: "welcome", tune: func(options *tui.Options) { options.Fresh, options.Pose = true, sittingPose }, beats: []beat{
			{"sitting", func(*reel) {}},
			{"typed", func(r *reel) { r.driver.Type(welcomeDraft) }},
			{"lying", func(r *reel) { r.options.Pose = lyingPose; r.open() }},
		}},
		{name: "sub-agents", beats: []beat{
			{"overview", func(r *reel) { r.send(agentWork(r.at)...); r.press("alt+2") }},
			{"waiting-agent", keys("left", "down", "down", "down")},
			{"dead-agent", keys("down")},
		}},
		{name: "file-edits", beats: []beat{
			{"index", func(r *reel) { r.send(fileWork()...); r.press("alt+3") }},
			{"modified", keys("right", "enter", "n")},
			{"created-first-line", keys("n", "home")},
			{"created-last-line", keys("end")},
		}},
		{name: "shells", tune: withShells, beats: []beat{
			{"running", func(r *reel) { r.settle(); r.press("alt+4") }},
			{"exited", keys("down")},
			{"kill-confirm", keys("up", "k")},
		}},
		{name: "settings", beats: categories},
		{name: "theme", beats: []beat{
			{"choice-open", func(r *reel) { openSettings(r); r.press("enter") }},
			{"previewing", keys("down", "down", "down")},
		}},
		{name: "settings-search", beats: []beat{
			{"open", func(r *reel) { openSettings(r); r.press("ctrl+k") }},
			{"typed", func(r *reel) { r.driver.Type("diff") }},
		}},
		{name: "models", beats: []beat{
			{"models", keys("ctrl+l")},
			{"roles", keys("tab")},
		}},
		{name: "quota", beats: []beat{
			{"open", func(r *reel) { r.driver.Type("/status"); r.press("enter") }},
		}},
		{name: "commands", beats: []beat{
			{"open", keys("alt+k")},
			{"filtered", func(r *reel) { r.driver.Type("file") }},
		}},
		{name: "search", beats: []beat{
			{"open", func(r *reel) { r.send(agentWork(r.at)...); r.press("ctrl+k") }},
			{"typed", func(r *reel) { r.driver.Type("ledger") }},
		}},
		{name: "file-picker", beats: []beat{
			{"open", func(r *reel) { r.driver.Type("read "); r.press("@"); r.settle() }},
			{"moved", keys("down")},
		}},
		{name: "hostkeys", beats: []beat{
			{"keybindings", func(r *reel) { openSettings(r); r.press("right", "right", "down", "enter") }},
			{"host", keys("esc", "down", "enter")},
		}},
		{name: "narrow", beats: []beat{
			{"chat", func(r *reel) {
				r.driver.Resize(narrowWidth, narrowHeight)
				r.driver.Type(welcomeDraft)
				r.press("enter")
				r.send(agentWork(r.at)...)
				r.app.Update(tui.Closed{})
			}},
			{"sub-agents", keys("alt+2")},
		}},
	}
}
