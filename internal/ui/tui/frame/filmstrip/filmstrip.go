package filmstrip

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	roster "tofu/internal/agent/subagents"
	"tofu/internal/host"
	library "tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/core"
	isettings "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/ui/tui/app"
	"tofu/internal/ui/tui/chat/session"
	"tofu/internal/ui/tui/fixture"
)

const (
	nameSeparator = "/"
	numberDigits  = 2
	beatGap       = 900 * time.Millisecond
	toolGap       = 1400 * time.Millisecond
	typingGap     = 3 * time.Second
	subAgentGap   = 26 * time.Second
	subAgentID    = "qa-1"
	homePrefix    = "tofu-filmstrip"
	workspaceDir  = "tofu"
	keymapFile    = "keybindings.json"
	hyperlinksOff = "off"
	pushFailure   = "git push origin develop: exit 128\nfatal: could not read from remote repository, make sure you have the right access\ntransport: ssh: connect to host git.silo port 22: connection refused"
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
	tune  func(*app.Options)
	beats []beat
}

type reel struct {
	app     *app.App
	driver  *Driver
	at      time.Time
	options app.Options
	width   int
	height  int
}

func workspaceFiles() []string {
	return []string{"README.md", "go.mod", "internal/agent/loop/loop.go", "internal/judge/policy/resolve.go", "apps/cli/main.go"}
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

func newReel(width, height int, home string, tune func(*app.Options)) *reel {
	root := workspace(home)
	store, err := isettings.Open(filepath.Join(home, sys.StateDirName, isettings.FileName), filepath.Join(root, sys.StateDirName, isettings.FileName))
	if err == nil {
		err = store.SetText(isettings.Global, isettings.Hyperlinks, hyperlinksOff)
	}
	if err != nil {
		panic(err)
	}
	r := &reel{at: fixture.Opened(), width: width, height: height}
	r.options = app.Options{
		Repo:     fixture.Path,
		Root:     root,
		Branch:   fixture.Branch,
		Release:  fixture.Release,
		Settings: store,
		Keymap:   filepath.Join(home, keymapFile),
		Now:      func() time.Time { return r.at },
		Paths:    workspaceFiles,
		Wires: func() []app.Wire {
			return []app.Wire{{Name: fixture.Wire, Model: fixture.Model, Provider: fixture.Provider, Efforts: []core.Effort{core.EffortLow, core.EffortMedium, core.EffortHigh}}}
		},
		Turn:         func(context.Context, app.Pick, string, app.CalledFromInsideTheTurnAndNeverAfterItReturns) {},
		Copy:         func(string) error { return nil },
		ReloadModels: servedInCatalog,
	}
	if tune != nil {
		tune(&r.options)
	}
	r.open()
	return r
}

func servedInCatalog() string {
	catalog, err := library.CatalogDir()
	path := filepath.Join(catalog, "models", "anthropic", "claude-sonnet-5-5.yaml")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o700)
	}
	if err == nil {
		err = os.WriteFile(path, []byte("subscription: claude-sub\nuse: allowed\nfrom: listed by the claude-sub account\n"), 0o600)
	}
	if err != nil {
		return err.Error()
	}
	return "models reload found claude-sub/claude-sonnet-5-5"
}

func (r *reel) open() {
	if r.driver != nil {
		r.driver.Close()
	}
	r.app = app.New(r.options)
	r.driver = Drive(r.app, r.width, r.height, filepath.Join(r.options.Root, sys.StateDirName))
	r.app.Update(app.Event{Kind: app.EventSession, Text: fixture.SessionName, ID: fixture.SessionID})
	r.app.Update(fixture.Quotas(r.at))
	r.app.Update(app.Event{Kind: app.EventContext, Context: fixture.Context()})
	r.app.Update(app.Event{Kind: app.EventStats, TokensIn: 284000, TokensOut: 61000, CacheRead: 190000, Decisions: 3})
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

func (r *reel) send(events ...app.Event) {
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

func call(id, tool, text string) app.Event {
	return app.Event{Kind: app.EventToolCall, ID: id, Tool: tool, Text: text}
}

func result(id, text string) app.Event {
	return app.Event{Kind: app.EventToolResult, ID: id, Text: text}
}

func delta(text string) app.Event { return app.Event{Kind: app.EventTextDelta, Text: text} }

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
			r.send(app.Event{Kind: app.EventRequesting})
			r.wait(beatGap)
		}},
		beat{"thinking", func(r *reel) {
			r.send(app.Event{Kind: app.EventStats, TokensIn: 291000, TokensOut: 61400, CacheRead: 190000, Decisions: 3})
			r.wait(beatGap)
		}},
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "read", "internal/judge/policy/toolgate.go"), app.Event{Kind: app.EventDecision, Decision: allowedDecision()})
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
				app.Event{Kind: app.EventDone, Text: "cooked for"},
			)
			r.app.Update(app.Closed{})
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
			r.send(call("c"+strconv.Itoa(step), "read", "internal/agent/loop/step"+strconv.Itoa(step)+".go"))
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
				app.Event{Kind: app.EventText, ID: "a1f3c9", Text: "twelve reads and the loop only ever shows the one that is running."},
				app.Event{Kind: app.EventDone, Text: "cooked for"})
			r.app.Update(app.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func markdownAnswer() scenario {
	return scenario{name: "markdown", beats: append(opening(),
		beat{"requesting", func(r *reel) {
			r.send(app.Event{Kind: app.EventRequesting})
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
			r.send(delta("none of the three is knowable from the wire alone."), app.Event{Kind: app.EventDone, Text: "cooked for"})
			r.app.Update(app.Closed{})
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
			r.send(app.Event{Kind: app.EventDecision, Decision: askedDecision()})
			r.wait(beatGap)
		}},
		beat{"waiting", func(r *reel) {
			r.send(app.Event{Kind: app.EventAwaitPerson})
			r.wait(toolGap)
		}},
		beat{"allowed", func(r *reel) {
			r.app.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
			r.send(result("c1", "everything up to date"))
			r.wait(beatGap)
		}},
		beat{"answered", func(r *reel) {
			r.send(
				app.Event{Kind: app.EventText, ID: "b7e201", Text: "the force push was the only way to move the branch, and it is done."},
				app.Event{Kind: app.EventDone, Text: "cooked for"},
			)
			r.app.Update(app.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func interruptedTurn() scenario {
	return scenario{name: "interrupted", beats: append(opening(),
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "read", "internal/agent/loop/loop.go"))
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
			r.send(app.Event{Kind: app.EventDone, Text: "cooked for"})
			r.app.Update(app.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func lettingToolsFinish() scenario {
	return scenario{name: "letting-tools-finish", beats: append(opening(),
		beat{"tooling", func(r *reel) {
			r.send(call("c1", "read", "internal/agent/loop/loop.go"))
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
			r.send(result("c2", "ok tofu/internal/agent/loop 0.42s"), app.Event{Kind: app.EventDone, Text: "cooked for"})
			r.app.Update(app.Closed{})
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
			r.send(app.Event{Kind: app.EventToolResult, ID: "c1", Text: "exit 128", Outcome: host.ToolFailed})
			r.wait(beatGap)
		}},
		beat{"failed", func(r *reel) {
			r.send(app.Event{Kind: app.EventFailure, Tool: "bash", Text: pushFailure})
			r.wait(beatGap)
		}},
		beat{"closed", func(r *reel) {
			r.send(app.Event{Kind: app.EventDone, Text: "failed after"})
			r.app.Update(app.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func subAgentTurn() scenario {
	held, spent := &roster.Roster{}, map[string]int{}
	subAgent := func(r *reel, state roster.State, steps, tokens int) app.Event {
		held.Stepped(subAgentID, steps, r.at)
		held.Reached(subAgentID, state, "")
		spent[subAgentID] = tokens
		return app.Event{Kind: app.EventSubAgent, SubAgents: host.SubAgentRows(held.SubAgents(), r.at, 0, spent, nil)}
	}
	return scenario{name: "sub-agent", beats: append(opening(),
		beat{"spawned", func(r *reel) {
			_ = held.Hold(roster.SubAgent{
				ID:      subAgentID,
				Mission: "read the policy loader",
				Owns:    []string{"internal/judge/policy/**"},
				Started: r.at,
			})
			r.send(
				delta("handing the policy loader to a sub-agent"),
				app.Event{Kind: app.EventToolCall, ID: "42e30c", Tool: "spawn", Text: "read internal/judge/policy/resolve.go and say what it reads first", Promote: true},
				subAgent(r, roster.Working, 1, 0),
			)
			r.wait(subAgentGap)
		}},
		beat{"sub-agent-working", func(r *reel) {
			r.send(call("815e77", "read", "internal/judge/policy/resolve.go"), subAgent(r, roster.Working, 2, 9400))
			r.wait(subAgentGap)
		}},
		beat{"sub-agent-thinking", func(r *reel) {
			r.send(result("815e77", "209 lines, 6.2 KB"), subAgent(r, roster.Working, 2, 18200))
			r.wait(subAgentGap)
		}},
		beat{"answered", func(r *reel) {
			r.send(
				subAgent(r, roster.InReview, 2, 24600),
				result("42e30c", "3 lines, 199 bytes"),
				app.Event{Kind: app.EventText, ID: "d41c08", Text: "the sub-agent read it: the loader reads the lock before the mode."},
				app.Event{Kind: app.EventDone, Text: "cooked for"},
			)
			r.app.Update(app.Closed{})
			r.wait(beatGap)
		}},
	)}
}

func scenarios() []scenario {
	return append([]scenario{plainTurn(), twelveTools(), markdownAnswer(), askingTurn(), interruptedTurn(), lettingToolsFinish(), failedTurn(), subAgentTurn()}, screens()...)
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
	stub := map[string]string{"TERM_PROGRAM": "zed", "ZED_TERM": "", "VSCODE_PID": "", "WT_SESSION": "", "TERMINAL_EMULATOR": "", "TERM": "xterm-256color", "NO_COLOR": "", "TOFU_ASCII": ""}
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
