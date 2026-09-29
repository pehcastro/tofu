package filmstrip

import (
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
	library "tofu/internal/llm/models"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	driveDir         = "drive"
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
	mutex sync.Mutex
	text  strings.Builder
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

func seededTurn(at time.Time) tui.Turn {
	planned := seeds(at)
	return func(_ context.Context, _ tui.Pick, task string, emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {
		events, known := planned[task]
		if !known {
			events = append([]tui.Event{{Kind: tui.EventText, ID: "a0" + strconv.Itoa(len(task)), Text: "noted: " + task}}, done()...)
		}
		for _, event := range events {
			emit(event)
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
		if err := os.WriteFile(filepath.Join(home, globalSettings), planted, 0o600); err != nil {
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
	out := &transcript{}
	r := newReel(fixture.Width, fixture.Height, home, func(options *tui.Options) {
		signed := options.Wires
		options.Wires = func() []tui.Wire {
			if stored, _ := sys.StoredKeys(); stored[library.Meta.KeyName()] != "" {
				return append(signed(), tui.Wire{Name: string(library.Meta), Model: "muse-spark-1.3", Provider: string(library.Meta)})
			}
			return signed()
		}
		options.Fresh = strings.HasPrefix(name, freshPrefix)
		options.Turn = seededTurn(options.Now())
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
