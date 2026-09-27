package filmstrip

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui"
	"tofu/interface/tui/fixture"
	"tofu/interface/tui/session"
	"tofu/interface/tui/subagent"
	"tofu/internal/llm"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
)

const (
	nameSeparator  = "/"
	numberDigits   = 2
	beatGap        = 900 * time.Millisecond
	toolGap        = 1400 * time.Millisecond
	typingGap      = 3 * time.Second
	childGap       = 26 * time.Second
	childID        = "qa-1"
	homePrefix     = "tofu-filmstrip"
	workspaceDir   = "tofu"
	globalSettings = "settings.json"
	keymapFile     = "keybindings.json"
	hyperlinksOff  = "off"
	pushFailure    = "git push origin develop: exit 128\nfatal: could not read from remote repository, make sure you have the right access\ntransport: ssh: connect to host git.silo port 22: connection refused"
)

type Frame struct {
	Scenario string
	Name     string
	Content  string
}

type beat struct {
	name string
	play func(*reel)
}

type scenario struct {
	name  string
	tune  func(*tui.Options)
	beats []beat
}

type reel struct {
	app     *tui.App
	driver  *Driver
	at      time.Time
	options tui.Options
	width   int
	height  int
}

func workspaceFiles() []string {
	return []string{"README.md", "go.mod", "internal/turn/loop.go", "internal/judge/policy/resolve.go", "cmd/tofu/main.go"}
}

func workspace(home string) string {
	root := filepath.Join(home, workspaceDir)
	for _, file := range workspaceFiles() {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, []byte(file+"\n"), 0o600); err != nil {
			panic(err)
		}
	}
	return root
}

func newReel(width, height int, home string, tune func(*tui.Options)) *reel {
	root := workspace(home)
	store, err := isettings.Open(filepath.Join(home, globalSettings), filepath.Join(root, ".tofu", globalSettings))
	if err == nil {
		err = store.SetText(isettings.Global, isettings.Hyperlinks, hyperlinksOff)
	}
	if err != nil {
		panic(err)
	}
	r := &reel{at: fixture.Opened(), width: width, height: height}
	r.options = tui.Options{
		Repo:     fixture.Path,
		Root:     root,
		Branch:   fixture.Branch,
		Release:  fixture.Release,
		Settings: store,
		Keymap:   filepath.Join(home, keymapFile),
		Now:      func() time.Time { return r.at },
		Paths:    workspaceFiles,
		Wires: func() []tui.Wire {
			return []tui.Wire{{Name: fixture.Wire, Model: fixture.Model, Provider: fixture.Provider, Efforts: []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh}}}
		},
		Turn:    func(context.Context, tui.Pick, string, tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {},
		Answers: make(chan tui.Answer, 1),
		Copy:    func(string) error { return nil },
	}
	if tune != nil {
		tune(&r.options)
	}
	r.open()
	return r
}

func (r *reel) open() {
	if r.driver != nil {
		r.driver.Close()
	}
	r.app = tui.New(r.options)
	r.driver = Drive(r.app, r.width, r.height)
	r.app.Update(tui.Event{Kind: tui.EventSession, Text: fixture.SessionName, ID: fixture.SessionID})
	r.app.Update(fixture.Quotas(r.at))
	r.app.Update(tui.Event{Kind: tui.EventContext, Context: fixture.Context()})
	r.app.Update(tui.Event{Kind: tui.EventStats, TokensIn: 284000, TokensOut: 61000, CacheRead: 190000, Decisions: 3})
}

func (r *reel) settle() {
	if err := r.driver.Settle(); err != nil {
		panic(err)
	}
}

func (r *reel) press(keys ...string) {
	for _, key := range keys {
		if err := r.driver.Press(key); err != nil {
			panic(err)
		}
	}
}

func (r *reel) wait(d time.Duration) { r.at = r.at.Add(d) }

func (r *reel) send(events ...tui.Event) {
	for _, event := range events {
		r.app.Update(event)
	}
}

func askedDecision() *session.Decision {
	return &session.Decision{
		Tool:    "bash",
		Verdict: session.Ask,
		Answers: []session.Answer{
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

func allowedDecision() *session.Decision {
	return &session.Decision{
		Tool:    "read",
		Verdict: session.Allow,
		Answers: []session.Answer{{Question: "risk", Value: 0, Max: 3}},
		Reason: session.Reason{
			Question:  "risk",
			Limit:     "risk_ask_at",
			Levels:    fixture.RiskLevels(),
			Threshold: 1.5,
			Value:     0,
		},
	}
}

func call(id, tool, text string) tui.Event {
	return tui.Event{Kind: tui.EventToolCall, ID: id, Tool: tool, Text: text}
}

func result(id, text string) tui.Event {
	return tui.Event{Kind: tui.EventToolResult, ID: id, Text: text}
}

func delta(text string) tui.Event { return tui.Event{Kind: tui.EventTextDelta, Text: text} }

func opening() []beat {
	return []beat{
		{"fresh", func(r *reel) { r.wait(typingGap) }},
		{"typing", func(r *reel) { r.driver.Type(fixture.Task); r.wait(beatGap) }},
		{"sent", func(r *reel) { r.app.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); r.wait(beatGap) }},
	}
}

func plainTurn() scenario {
	return scenario{name: "plain", beats: append(opening(),
		beat{"requesting", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventRequesting})
			r.wait(beatGap)
		}},
		beat{"thinking", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventStats, TokensIn: 291000, TokensOut: 61400, CacheRead: 190000, Decisions: 3})
			r.wait(beatGap)
		}},
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "read", "internal/judge/policy/toolgate.go"), tui.Event{Kind: tui.EventDecision, Decision: allowedDecision()})
			r.wait(toolGap)
		}},
		beat{"second-tool", func(r *reel) {
			r.send(result("c1", "412 lines, 11.8 KB"), call("c2", "bash", "go test ./internal/judge/..."))
			r.wait(toolGap)
		}},
		beat{"streaming", func(r *reel) {
			r.send(result("c2", "ok tofu/internal/judge 0.42s"), delta("the gate reads the policy before the wire, because a policy "))
			r.wait(beatGap)
		}},
		beat{"answered", func(r *reel) {
			r.send(
				delta("that is not resolved cannot say whether a verdict is enforced."),
				tui.Event{Kind: tui.EventDone, Text: "cooked for"},
			)
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func reads(from, to int) func(*reel) {
	return func(r *reel) {
		for step := from; step <= to; step++ {
			if step > 1 {
				r.send(result("c"+strconv.Itoa(step-1), strconv.Itoa(step*37)+" lines, 4.1 KB"))
			}
			r.send(call("c"+strconv.Itoa(step), "read", "internal/turn/step"+strconv.Itoa(step)+".go"))
			r.wait(toolGap)
		}
	}
}

func twelveTools() scenario {
	return scenario{name: "twelve-tools", beats: append(opening(),
		beat{"first-tool", reads(1, 1)},
		beat{"fourth-tool", reads(2, 4)},
		beat{"eighth-tool", reads(5, 8)},
		beat{"twelfth-tool", reads(9, 12)},
		beat{"answered", func(r *reel) {
			r.send(result("c12", "84 lines, 2.1 KB"),
				tui.Event{Kind: tui.EventText, ID: "a1f3c9", Text: "twelve reads and the loop only ever shows the one that is running."},
				tui.Event{Kind: tui.EventDone, Text: "cooked for"})
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func markdownAnswer() scenario {
	return scenario{name: "markdown", beats: append(opening(),
		beat{"requesting", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventRequesting})
			r.wait(beatGap)
		}},
		beat{"first-paragraph", func(r *reel) {
			r.send(delta("the gate reads the policy first so that a verdict is never invented.\n\n"))
			r.wait(beatGap)
		}},
		beat{"heading", func(r *reel) {
			r.send(delta("## what the policy carries\n\n"))
			r.wait(beatGap)
		}},
		beat{"list", func(r *reel) {
			r.send(delta("- the mode it was declared in\n- the lock that resolved it\n- the thresholds\n\n"))
			r.wait(beatGap)
		}},
		beat{"answered", func(r *reel) {
			r.send(delta("none of the three is knowable from the wire alone."), tui.Event{Kind: tui.EventDone, Text: "cooked for"})
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func askingTurn() scenario {
	return scenario{name: "asking", beats: append(opening(),
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "bash", "git push --force origin develop"))
			r.wait(toolGap)
		}},
		beat{"asked", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventDecision, Decision: askedDecision()})
			r.wait(beatGap)
		}},
		beat{"waiting", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventAwaitPerson})
			r.wait(toolGap)
		}},
		beat{"allowed", func(r *reel) {
			r.app.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
			r.send(result("c1", "everything up to date"))
			r.wait(beatGap)
		}},
		beat{"answered", func(r *reel) {
			r.send(
				tui.Event{Kind: tui.EventText, ID: "b7e201", Text: "the force push was the only way to move the branch, and it is done."},
				tui.Event{Kind: tui.EventDone, Text: "cooked for"},
			)
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func interruptedTurn() scenario {
	return scenario{name: "interrupted", beats: append(opening(),
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "read", "internal/turn/loop.go"))
			r.wait(toolGap)
		}},
		beat{"answering", func(r *reel) {
			r.send(result("c1", "84 lines, 2.1 KB"),
				delta("the loop reads the policy first, then the wire, because a locked"))
			r.wait(beatGap)
		}},
		beat{"stopping", func(r *reel) {
			r.app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			r.wait(beatGap)
		}},
		beat{"stopped", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventDone, Text: "cooked for"})
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func lettingToolsFinish() scenario {
	return scenario{name: "letting-tools-finish", beats: append(opening(),
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "read", "internal/turn/loop.go"))
			r.wait(toolGap)
		}},
		beat{"second-tool", func(r *reel) {
			r.send(result("c1", "84 lines, 2.1 KB"), call("c2", "bash", "go test ./..."))
			r.wait(toolGap)
		}},
		beat{"stopping-the-model", func(r *reel) {
			r.app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			r.wait(beatGap)
		}},
		beat{"stopped", func(r *reel) {
			r.send(result("c2", "ok tofu/internal/turn 0.42s"), tui.Event{Kind: tui.EventDone, Text: "cooked for"})
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func failedTurn() scenario {
	return scenario{name: "failed", beats: append(opening(),
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "bash", "git push origin develop"))
			r.wait(toolGap)
		}},
		beat{"failing", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventToolResult, ID: "c1", Text: "exit 128", Failed: true})
			r.wait(beatGap)
		}},
		beat{"failed", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventFailure, Tool: "bash", Text: pushFailure})
			r.wait(beatGap)
		}},
		beat{"closed", func(r *reel) {
			r.send(tui.Event{Kind: tui.EventDone, Text: "failed after"})
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func childTurn() scenario {
	held, spent := &roster.Roster{}, map[string]int{}
	child := func(r *reel, state roster.State, steps, tokens int) tui.Event {
		held.Stepped(childID, steps, r.at)
		held.Reached(childID, state, "")
		spent[childID] = tokens
		return tui.Event{Kind: tui.EventSubAgent, Children: subagent.Children(held.SubAgents(), r.at, 0, spent, nil)}
	}
	return scenario{name: "child", beats: append(opening(),
		beat{"spawned", func(r *reel) {
			_ = held.Hold(roster.SubAgent{
				ID:      childID,
				Mission: "read the policy loader",
				Owns:    []string{"internal/judge/policy/**"},
				Started: r.at,
			})
			r.send(
				delta("handing the policy loader to a child"),
				tui.Event{Kind: tui.EventToolCall, ID: "42e30c", Tool: "spawn", Text: "read internal/judge/policy/resolve.go and say what it reads first", Promote: true},
				child(r, roster.Working, 1, 0),
			)
			r.wait(childGap)
		}},
		beat{"child-working", func(r *reel) {
			r.send(call("815e77", "read", "internal/judge/policy/resolve.go"), child(r, roster.Working, 2, 9400))
			r.wait(childGap)
		}},
		beat{"child-thinking", func(r *reel) {
			r.send(result("815e77", "209 lines, 6.2 KB"), child(r, roster.Working, 2, 18200))
			r.wait(childGap)
		}},
		beat{"answered", func(r *reel) {
			r.send(
				child(r, roster.InReview, 2, 24600),
				result("42e30c", "3 lines, 199 bytes"),
				tui.Event{Kind: tui.EventText, ID: "d41c08", Text: "the child read it: the loader reads the lock before the mode."},
				tui.Event{Kind: tui.EventDone, Text: "cooked for"},
			)
			r.app.Update(tui.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func scenarios() []scenario {
	return append([]scenario{plainTurn(), twelveTools(), markdownAnswer(), askingTurn(), interruptedTurn(), lettingToolsFinish(), failedTurn(), childTurn()}, screens()...)
}

func frameName(scenarioName string, index int, beatName string) string {
	number := strconv.Itoa(index + 1)
	if len(number) < numberDigits {
		number = "0" + number
	}
	return scenarioName + nameSeparator + number + "-" + beatName
}

func Names() []string {
	var names []string
	for _, one := range scenarios() {
		for index, step := range one.beats {
			names = append(names, frameName(one.name, index, step.name))
		}
	}
	return names
}

func stubbedHost(home string) (restore func()) {
	stub := map[string]string{"TERM_PROGRAM": "zed", "ZED_TERM": "", "VSCODE_PID": "", "WT_SESSION": "", "TERMINAL_EMULATOR": ""}
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE", "HOME", "XDG_CONFIG_HOME"} {
		stub[variable] = home
	}
	var undo []func()
	for variable, value := range stub {
		held, set := os.LookupEnv(variable)
		undo = append(undo, func() {
			if set {
				_ = os.Setenv(variable, held)
				return
			}
			_ = os.Unsetenv(variable)
		})
		if err := os.Setenv(variable, value); err != nil {
			panic(err)
		}
	}
	return func() {
		for _, step := range undo {
			step()
		}
	}
}

func play(chosen []scenario, width, height int) []Frame {
	home, err := os.MkdirTemp("", homePrefix)
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(home) }()
	defer stubbedHost(home)()
	var frames []Frame
	for index, one := range chosen {
		r := newReel(width, height, filepath.Join(home, strconv.Itoa(index)), one.tune)
		for number, step := range one.beats {
			step.play(r)
			frames = append(frames, Frame{Scenario: one.name, Name: frameName(one.name, number, step.name), Content: r.app.View().Content})
		}
		r.driver.Close()
	}
	return frames
}

func All(width, height int) []Frame { return play(scenarios(), width, height) }

func Find(name string, width, height int) (Frame, bool) {
	scenarioName, _, _ := strings.Cut(name, nameSeparator)
	for _, one := range scenarios() {
		if one.name != scenarioName {
			continue
		}
		for _, frame := range play([]scenario{one}, width, height) {
			if frame.Name == name {
				return frame, true
			}
		}
	}
	return Frame{}, false
}
