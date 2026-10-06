package filmstrip

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
	"tofu/interface/tui/fixture"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/subagent"
	"tofu/internal/golden"
	"tofu/internal/host"
	"tofu/internal/konst"
	library "tofu/internal/llm/models"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	driveDir         = "drive"
	cassetteDir      = "cassettes"
	stoppedLeadWords = "cancelled at"
	scriptSuffix     = ".txt"
	settingsSuffix   = ".settings.json"
	freshPrefix      = "fresh-"
	asciiPrefix      = "ascii-"
	rawPrefix        = "raw-"
	driveTimeout     = 10 * time.Second
	tallLines        = 60
	clipboardRepeats = 6
	acceptedMetaKey  = "meta-made-up-accepted"
	silentMetaKey    = "meta-made-up-silent"
)

type transcript struct {
	mutex   sync.Mutex
	text    strings.Builder
	release chan struct{}
}

func (t *transcript) Write(p []byte) (int, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.text.Write(p)
}

func (t *transcript) String() string {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.text.String()
}

func seeds(at time.Time) map[string][]tui.Event {
	work := agentWork(at)
	var subAgents []subagent.Row
	for _, event := range work {
		if event.Kind == tui.EventSubAgent {
			subAgents = event.SubAgents
		}
	}
	spawned := slices.Concat(subAgents, []subagent.Row{{Name: "c4", Doing: "audit the shell registry", Owns: []string{"internal/shell/registry.go"}, Since: time.Second, Steps: 1, Total: 40, State: roster.Working}})
	tall := make([]string, tallLines)
	for index := range tall {
		tall[index] = "line " + strconv.Itoa(index+1) + " of the tall result"
	}
	return map[string][]tui.Event{
		"agents": work,
		"ask":    askWork(),
		"edits":  fileWork(),
		"spawn": append([]tui.Event{
			{Kind: tui.EventToolCall, ID: "5e04aa", Tool: "spawn", Text: "audit the shell registry", Promote: true},
			{Kind: tui.EventSubAgent, SubAgents: spawned},
			result("5e04aa", "spawned [&c4]"),
		}, done()...),
		"more": append([]tui.Event{call("6f1a01", "read", "internal/turn/budget.go"), result("6f1a01", "88 lines, 2.4 KB")}, done()...),
		"tall": append([]tui.Event{call("6f2b01", "bash", "go test -v ./internal/turn/..."), result("6f2b01", strings.Join(tall, "\n"))}, done()...),
	}
}

type cassette struct {
	at       time.Time
	release  <-chan struct{}
	stopLead <-chan struct{}
	ended    context.Context
	emit     func(tui.Event)
	rows     []subagent.Row
}

func (c *cassette) row(name string) *subagent.Row {
	at := slices.IndexFunc(c.rows, func(row subagent.Row) bool { return row.Name == name })
	if at < 0 {
		c.rows = append(c.rows, subagent.Row{Name: name, Total: konst.TurnMaxSteps})
		at = len(c.rows) - 1
	}
	return &c.rows[at]
}

func (c *cassette) snapshot() []subagent.Row {
	rows := slices.Clone(c.rows)
	for index := range rows {
		rows[index].Calls = slices.Clone(rows[index].Calls)
	}
	return rows
}

func (c *cassette) released(ctx context.Context, release <-chan struct{}) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-c.ended.Done():
			return false
		case <-release:
			return true
		case <-c.stopLead:
			c.emit(tui.Event{Kind: tui.EventDone, Text: stoppedLeadWords})
		}
	}
}

func rosterState(name string) roster.State {
	states := roster.States()
	at := slices.IndexFunc(states, func(state roster.State) bool { return state.String() == name })
	if at < 0 {
		panic("cassette: no sub-agent state is named " + name)
	}
	return states[at]
}

func (c *cassette) play(ctx context.Context, body string) {
	for _, step := range ReadScript(body) {
		agent, verb, text := "", step.Verb, step.Text
		if name, tagged := strings.CutPrefix(verb, "@"); tagged {
			agent = name
			verb, text, _ = strings.Cut(text, " ")
		}
		id, rest, _ := strings.Cut(text, " ")
		word, tail, _ := strings.Cut(rest, " ")
		switch verb {
		case "requesting":
			c.emit(tui.Event{Kind: tui.EventRequesting})
		case "spawn":
			c.emit(tui.Event{Kind: tui.EventToolCall, ID: id, Tool: "spawn", Text: rest, Promote: true})
		case "call":
			c.emit(tui.Event{Kind: tui.EventToolCall, ID: id, Tool: word, Text: tail, Agent: agent})
		case "result":
			c.emit(tui.Event{Kind: tui.EventToolResult, ID: id, Text: rest, Agent: agent})
		case "thinking":
			c.emit(tui.Event{Kind: tui.EventThinking, ID: id, Text: rest, Agent: agent})
		case "stats":
			in, _ := strconv.Atoi(id)
			out, _ := strconv.Atoi(rest)
			c.emit(tui.Event{Kind: tui.EventStats, Agent: agent, TokensIn: in, TokensOut: out})
		case "text":
			c.emit(tui.Event{Kind: tui.EventText, ID: id, Text: rest})
		case "done":
			c.emit(tui.Event{Kind: tui.EventDone, Text: text, SubAgents: c.snapshot()})
		case "row", "report", "pending":
			row := c.row(id)
			switch verb {
			case "row":
				row.State, row.Doing = rosterState(word), cmp.Or(tail, row.Doing)
			case "report":
				row.State, row.Report = rosterState(word), tail
			case "pending":
				ago, err := time.ParseDuration(word)
				if err != nil {
					panic("cassette line " + strconv.Itoa(step.Line) + ": " + err.Error())
				}
				callID, call, _ := strings.Cut(tail, " ")
				tool, what, _ := strings.Cut(call, " ")
				row.Calls = append(row.Calls, subagent.Call{ID: callID, At: c.at.Add(-ago), Tool: tool, Text: what})
			}
			c.emit(tui.Event{Kind: tui.EventSubAgent, SubAgents: c.snapshot()})
		case "pause":
			if !c.released(ctx, c.release) {
				return
			}
		case "hold":
			c.released(ctx, nil)
			return
		default:
			panic("cassette line " + strconv.Itoa(step.Line) + ": no event is named " + strconv.Quote(verb))
		}
	}
}

func seededTurn(at time.Time, played cassette) host.Play {
	planned := seeds(at)
	return func(ctx context.Context, _ tui.Pick, task string, live host.Live) {
		if body, err := os.ReadFile(filepath.Join("testdata", cassetteDir, task+scriptSuffix)); err == nil {
			played.at, played.emit, played.stopLead = at, live.Emit, live.LeadStop
			played.play(ctx, string(body))
			return
		}
		events, known := planned[task]
		if !known {
			events = append([]tui.Event{{Kind: tui.EventText, ID: "a0" + strconv.Itoa(len(task)), Text: "noted: " + task}}, done()...)
		}
		for _, event := range events {
			live.Emit(event)
		}
	}
}

func launch(t *testing.T, path string) (*reel, *transcript) {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(path), scriptSuffix)
	home := t.TempDir()
	t.Cleanup(stubbedHost(home))
	ascii := ""
	if strings.HasPrefix(name, asciiPrefix) {
		ascii = "1"
	}
	t.Setenv("TOFU_ASCII", ascii)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if planted, err := os.ReadFile(strings.TrimSuffix(path, scriptSuffix) + settingsSuffix); err == nil {
		if err := sys.WriteFile(filepath.Join(home, sys.StateDirName, isettings.FileName), planted, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metaModels := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch authorization := r.Header.Get("Authorization"); {
		case strings.HasPrefix(authorization, "Bearer "+silentMetaKey):
			<-r.Context().Done()
		case !strings.HasPrefix(authorization, "Bearer "+acceptedMetaKey):
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(metaModels.Close)
	t.Setenv(library.MetaBaseURLVariable, metaModels.URL)
	out := &transcript{release: make(chan struct{}, 1)}
	r := newReel(fixture.Width, fixture.Height, home, func(options *tui.Options) {
		signed := options.Wires
		options.Wires = func() []tui.Wire {
			if stored, _ := sys.StoredKeys(); stored[library.Meta.KeyName()] != "" {
				return append(signed(), tui.Wire{Name: string(library.Meta), Model: "muse-spark-1.3", Provider: string(library.Meta)})
			}
			return signed()
		}
		options.Fresh = strings.HasPrefix(name, freshPrefix)
		options.Host, _ = host.New(host.Config{Now: options.Now, Play: seededTurn(options.Now(), cassette{release: out.release, ended: t.Context()})})
		t.Cleanup(options.Host.Close)
		options.Copy = func(text string) error {
			_, err := out.Write([]byte("clipboard: " + text + "\n"))
			return err
		}
		options.Paste = paste.Board{Read: func() (sys.Clipboard, error) {
			return sys.Clipboard{Kind: sys.ClipboardText, Text: strings.Repeat("the clipboard holds a long passage. ", clipboardRepeats)}, nil
		}}
		withShells(options)
	})
	t.Cleanup(r.driver.Close)
	return r, out
}

func playAll(t *testing.T, r *reel, script string, plain bool, out *transcript) {
	t.Helper()
	width, height := fixture.Width, fixture.Height
	for _, step := range ReadScript(script) {
		switch step.Verb {
		case "clock", "release", "once", "before":
			if err := r.driver.settle(driveTimeout); err != nil {
				t.Fatalf("line %d: %v\n%s", step.Line, err, r.driver.Plain())
			}
			screen := r.driver.Plain()
			switch step.Verb {
			case "clock":
				passed, err := time.ParseDuration(step.Text)
				if err != nil {
					t.Fatalf("line %d: %v", step.Line, err)
				}
				r.at = r.at.Add(passed)
			case "release":
				select {
				case out.release <- struct{}{}:
				case <-time.After(driveTimeout):
					t.Fatalf("line %d: the cassette never took the last release", step.Line)
				}
			case "once":
				if count := strings.Count(screen, step.Text); count != 1 {
					t.Fatalf("line %d: %s is on the screen %d times, not once\n%s", step.Line, step.Text, count, screen)
				}
			case "before":
				above, below, _ := strings.Cut(step.Text, " | ")
				if at, later := strings.Index(screen, above), strings.Index(screen, below); at < 0 || later < 0 || at > later {
					t.Fatalf("line %d: %s is not drawn above %s\n%s", step.Line, above, below, screen)
				}
			}
			continue
		}
		if err := r.driver.Play(step, driveTimeout, plain, out); err != nil {
			t.Fatalf("line %d: %v\n%s", step.Line, err, r.driver.Plain())
		}
		switch step.Verb {
		case "resize":
			size := strings.Fields(step.Text)
			width, _ = strconv.Atoi(size[0])
			height, _ = strconv.Atoi(size[1])
		case "screen":
			fits(t, step.Line, r.driver.Plain(), width, height)
		}
	}
}

func drive(t *testing.T, path string) string {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(path), scriptSuffix)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, out := launch(t, path)
	playAll(t, r, string(body), !strings.HasPrefix(name, rawPrefix), out)
	if strings.HasPrefix(name, asciiPrefix) {
		for _, glyph := range out.String() {
			if glyph > '~' {
				t.Errorf("ASCII mode printed %q", glyph)
				break
			}
		}
	}
	return out.String()
}

func fits(t *testing.T, line int, screen string, width, height int) {
	t.Helper()
	rows := strings.Split(screen, "\n")
	if len(rows) != height {
		t.Errorf("line %d: the screen has %d rows at %dx%d", line, len(rows), width, height)
	}
	for index, row := range rows {
		if cells := ansi.StringWidth(row); cells > width {
			t.Errorf("line %d: row %d is %d cells wide at %dx%d\n%s", line, index, cells, width, height, row)
		}
	}
}

func TestEveryThemeRecoloursTheWholeCanvas(t *testing.T) {
	r, out := launch(t, "every-theme"+scriptSuffix)
	nextTheme := "type /settings\nkey enter\nkey enter\nkey down\nkey enter\nkey esc\n"
	var themes []string
	for _, spec := range isettings.Default() {
		if spec.Key == isettings.Theme {
			themes = spec.Choices
		}
	}
	painted, canvases := map[string]string{}, map[string]string{}
	for range themes {
		playAll(t, r, nextTheme, true, out)
		view := r.app.View()
		shown := r.options.Settings.Text(isettings.Theme)
		if earlier, seen := painted[view.Content]; seen {
			t.Errorf("%s paints the chat exactly as %s does", shown, earlier)
		}
		painted[view.Content] = shown
		canvas := fmt.Sprint(view.BackgroundColor)
		if earlier, seen := canvases[canvas]; seen {
			t.Errorf("%s paints the canvas %s, as %s does", shown, canvas, earlier)
		}
		canvases[canvas] = shown
	}
	if len(painted) != len(themes) {
		t.Errorf("%d themes were driven and %d distinct chats were painted", len(themes), len(painted))
	}
}

func TestTheTerminalThemeLeavesNoColourOnTheFreshChatArt(t *testing.T) {
	r, out := launch(t, freshPrefix+"theme"+scriptSuffix)
	playAll(t, r, "type /settings\nkey enter\nkey enter\nkey up\nkey enter\nkey esc\nwait ▀\n", true, out)
	if theme := r.options.Settings.Text(isettings.Theme); theme != "terminal" {
		t.Fatalf("the drive chose %s, not terminal", theme)
	}
	if painted := r.app.View().Content; strings.Contains(painted, "\x1b[") {
		t.Errorf("the fresh chat and its art still paint colour under the terminal theme")
	}
}

func TestEveryDriveScriptPrintsItsGolden(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("testdata", driveDir, "*"+scriptSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("no drive script was found")
	}
	for _, path := range scripts {
		name := strings.TrimSuffix(filepath.Base(path), scriptSuffix)
		t.Run(name, func(t *testing.T) {
			golden.Assert(t, filepath.Join(driveDir, name+goldenSuffix), drive(t, path))
		})
	}
}

func TestEveryWriteLandsInTheScopeItNamesAndNowhereElse(t *testing.T) {
	root := t.TempDir()
	dirs := [2]string{filepath.Join(root, "home", sys.StateDirName), filepath.Join(root, "project", sys.StateDirName)}
	store, err := isettings.Open(filepath.Join(dirs[isettings.Global], isettings.FileName), filepath.Join(dirs[isettings.Project], isettings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	writes := []struct {
		write isettings.Write
		file  string
		holds string
	}{
		{isettings.Write{Kind: isettings.WriteValue, Key: isettings.ChatShowsTools, Value: "1"}, isettings.FileName, `"chatShowsTools": 1`},
		{isettings.Write{Kind: isettings.WriteValue, Key: isettings.DecisionCap, Value: "7"}, isettings.FileName, `"decisionCap": 7`},
		{isettings.Write{Kind: isettings.WriteValue, Key: isettings.Theme, Value: "Nord"}, isettings.FileName, `"theme": "Nord"`},
		{isettings.Write{Kind: isettings.WriteValue, Key: isettings.BrowserModel, Value: "claude-sub/claude-sonnet-5"}, isettings.FileName, `"browserModel": "claude-sub/claude-sonnet-5"`},
		{isettings.Write{Kind: isettings.WriteRole, Key: "orchestrator", Value: "claude-sub/claude-sonnet-5"}, filepath.Join("roles", "orchestrator.yaml"), "model: claude-sub/claude-sonnet-5"},
		{isettings.Write{Kind: isettings.WriteRole, Key: "classifier", Value: "typesafe/jev-latest"}, filepath.Join("roles", "classifier.yaml"), "model: typesafe/jev-latest"},
		{isettings.Write{Kind: isettings.WriteSubAgent, Key: "go-dev", Value: "claude-sub/claude-sonnet-5"}, roster.AssignmentFile, "go-dev: claude-sub/claude-sonnet-5"},
	}
	for _, scope := range []isettings.Scope{isettings.Global, isettings.Project} {
		for _, each := range writes {
			if err := store.Save(scope, each.write); err != nil {
				t.Fatalf("%s %v: %v", scope, each.write, err)
			}
			if written, _ := os.ReadFile(filepath.Join(dirs[scope], each.file)); !strings.Contains(string(written), each.holds) {
				t.Errorf("%s %v: %s holds %q, want %s", scope, each.write, each.file, written, each.holds)
			}
			if leaked, _ := os.ReadFile(filepath.Join(dirs[isettings.Project], each.file)); scope == isettings.Global && len(leaked) > 0 {
				t.Errorf("a global %v also wrote the project %s: %q", each.write, each.file, leaked)
			}
		}
	}
	refused := []isettings.Write{
		{Kind: isettings.WriteRole, Key: "genius", Value: "claude-sub/claude-sonnet-5"},
		{Kind: isettings.WriteValue, Key: isettings.DecisionCap, Value: "seven"},
		{Kind: isettings.WriteValue, Key: "noSuchSetting", Value: "1"},
	}
	for _, write := range refused {
		if err := store.Save(isettings.Global, write); err == nil {
			t.Errorf("%v was saved", write)
		}
	}
	if _, err := os.Stat(filepath.Join(dirs[isettings.Global], "roles", "genius.yaml")); err == nil {
		t.Error("a refused role still wrote a file")
	}
	homeless, err := isettings.Open(filepath.Join(dirs[isettings.Global], isettings.FileName), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := homeless.Save(isettings.Project, isettings.Write{Kind: isettings.WriteRole, Key: "orchestrator", Value: "claude-sub/claude-sonnet-5"}); err == nil {
		t.Error("a role was saved into a scope with no path")
	}
}

func TestAPickAndASettingLandInTheScopeTheHeaderNames(t *testing.T) {
	r, out := launch(t, "scope-writes"+scriptSuffix)
	home, err := sys.HomeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(r.options.Root, sys.StateDirName)
	bound := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, "roles", "orchestrator.yaml"))
		return err == nil
	}
	pick := "key enter\nwait Select model\ntype claude-sonnet-5\nkey enter\nwait orchestrator now runs claude-sub/claude-sonnet-5\n"
	toggle := "key ctrl+k\ntype Read before edit\nkey enter\nkey enter\nkey down\nkey enter\n"
	playAll(t, r, "type /settings\nkey enter\nwait global scope (shift+tab)\nkey right\nwait Orchestrator model\n"+pick, true, out)
	if !bound(home) || bound(project) {
		t.Fatalf("global scope: home bound %v, project bound %v", bound(home), bound(project))
	}
	playAll(t, r, "key shift+tab\nwait project scope (shift+tab)\n"+pick, true, out)
	if !bound(project) {
		t.Fatal("project scope wrote no project role file")
	}
	playAll(t, r, "key shift+tab\nwait global scope (shift+tab)\n"+toggle+"key shift+tab\nwait project scope (shift+tab)\n"+toggle, true, out)
	for scope, want := range map[isettings.Scope]string{isettings.Global: `"readBeforeEdit": 0`, isettings.Project: `"readBeforeEdit": 1`} {
		body, err := os.ReadFile(r.options.Settings.Path(scope))
		if err != nil || !strings.Contains(string(body), want) {
			t.Errorf("the %s settings.json holds %q, want %s: %v", scope, body, want, err)
		}
	}
}
