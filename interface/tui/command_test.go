package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/palette"
	"tofu/internal/command"
	"tofu/internal/golden"
	"tofu/internal/host"
)

func commandApp(t *testing.T, entered chan<- string) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:       testRepo,
		Branch:     "develop",
		Now:        fixedClock(),
		Wires:      anthropicAlone,
		Copy:       func(string) error { return nil },
		Paths:      repoPaths,
		Sessions:   func() ([]SessionRow, error) { return nil, nil },
		Resume:     func(id string) (string, host.Opened) { return "continuing " + id, host.Opened{} },
		NewSession: func() string { return "the next task starts a new session" },
		Compact:    func(string) string { return "compacted 2 old tool result(s)" },
		Undo:       func(count string) string { return "undid " + count },
		Turn: func(_ context.Context, _ Pick, task string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			entered <- task
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func nothingEntered(t *testing.T, entered <-chan string) {
	t.Helper()
	select {
	case task := <-entered:
		t.Fatalf("the app sent %q to the model", task)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSlashSettingsOpensTheViewAndSendsNothingToTheModel(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/settings")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.current != screenSettings {
		t.Fatalf("/settings left the app on screen %d, want the settings screen", app.current)
	}
	nothingEntered(t, entered)
	if left := app.view.Value(); left != "" {
		t.Errorf("the composer still holds %q after the command ran", left)
	}
	content := ansi.Strip(app.View().Content)
	if containsAPlaceholder(content) {
		t.Errorf("the settings view still draws the composer\n%s", content)
	}
}

func TestSlashLinksOpensTheLinksDialog(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/links")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if _, open := app.top().(*recordedDialog); !open {
		t.Fatalf("/links opened %T, want the links dialog", app.top())
	}
	if content := ansi.Strip(app.View().Content); !strings.Contains(content, "Links") {
		t.Errorf("the links dialog is not drawn\n%s", content)
	}
	nothingEntered(t, entered)
}

func TestEnterOnAPickedLinkCopiesItAndComesBackToChat(t *testing.T) {
	copied := make(chan string, 1)
	app := newTestApp(Options{
		Repo: testRepo,
		Now:  fixedClock(),
		Copy: func(text string) error { copied <- text; return nil },
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	links := app.linksDialog()
	links.searchDialog = searchDialog{palette.NewSearch("Links", "", func(string) []palette.Result {
		return []palette.Result{{Label: "https://go.dev/doc", Detail: "you"}}
	})}
	app.push(links)
	cmd := app.update(tea.KeyPressMsg{Code: tea.KeyEnter})

	var copiedLink copiedMsg
	for pending := []tea.Cmd{cmd}; len(pending) > 0; pending = pending[1:] {
		if pending[0] == nil {
			continue
		}
		switch msg := pending[0]().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case copiedMsg:
			copiedLink = msg
		}
	}
	if copiedLink.text != "https://go.dev/doc" {
		t.Fatalf("enter produced %#v", copiedLink)
	}
	if got := <-copied; got != "https://go.dev/doc" {
		t.Fatalf("the clipboard was handed %q", got)
	}
	if app.current != screenChat || app.top() != nil {
		t.Fatalf("copying a link left the app on screen %d with %T open, want chat", app.current, app.top())
	}
}

func TestASlashListsTheCommandsAndTypingFiltersTheList(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/")
	listed := ansi.Strip(app.View().Content)
	for _, want := range []string{"/chat", "/sub-agents", "/settings", "/quit"} {
		if !strings.Contains(listed, want) {
			t.Errorf("a bare slash does not list %s\n%s", want, listed)
		}
	}

	typeText(app, "co")
	filtered := ansi.Strip(app.View().Content)
	for _, want := range []string{"/copy", "/copy-call"} {
		if !strings.Contains(filtered, want) {
			t.Errorf("/co does not list %s\n%s", want, filtered)
		}
	}
	for _, gone := range []string{"/sub-agents", "/quit"} {
		if strings.Contains(filtered, gone) {
			t.Errorf("/co still lists %s\n%s", gone, filtered)
		}
	}
	nothingEntered(t, entered)
}

func TestEveryListedCommandRunsAndNoneIsAName(t *testing.T) {
	for _, known := range commandApp(t, make(chan string, 1)).view.Commands {
		t.Run(known.Name, func(t *testing.T) {
			entered := make(chan string, 1)
			app := commandApp(t, entered)
			typeText(app, "/"+known.Name)
			app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if content := ansi.Strip(app.View().Content); strings.Contains(content, "there is no /") {
				t.Fatalf("/%s is listed and does nothing\n%s", known.Name, content)
			}
			if !slices.ContainsFunc(command.Table(), func(one command.Command) bool { return one.Name == known.Name && one.Starts() }) {
				nothingEntered(t, entered)
			}
		})
	}
}

func TestSlashNewIsRefusedWhileATurnRuns(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "write a note")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if task := <-entered; task != "write a note" {
		t.Fatalf("the turn never started, the model was sent %q", task)
	}
	typeText(app, "/new")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, turnRunningNote) {
		t.Errorf("/new during a turn does not say to stop the turn first\n%s", content)
	}
	if strings.Contains(content, "starts a new session") {
		t.Errorf("/new dropped what a running turn is carrying\n%s", content)
	}
}

func TestSlashCompactSaysWhatItShrankAndIsRefusedWhileATurnRuns(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	compacted := 0
	app.options.Compact = func(string) string { compacted++; return "compacted 2 old tool result(s)" }
	typeText(app, "/compact")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if content := ansi.Strip(app.View().Content); compacted != 1 || !strings.Contains(content, "compacted 2 old tool result(s)") {
		t.Fatalf("/compact ran %d time(s) and the chat does not say what it shrank\n%s", compacted, content)
	}
	nothingEntered(t, entered)

	typeText(app, "write a note")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if task := <-entered; task != "write a note" {
		t.Fatalf("the turn never started, the model was sent %q", task)
	}
	typeText(app, "/compact")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if content := ansi.Strip(app.View().Content); compacted != 1 || !strings.Contains(content, turnRunningNote) {
		t.Errorf("/compact during a turn ran %d time(s), want once and the note to stop the turn first\n%s", compacted, content)
	}
}

func TestTheOpenMenuGolden(t *testing.T) {
	app := commandApp(t, make(chan string, 1))
	typeText(app, "/se")
	golden.Assert(t, "session-menu-80x24.golden", app.View().Content)
}

func TestTheMenuMovesCompletesAndCloses(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/cop")
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	picked, open := app.view.Picked()
	if !open || picked != "copy-call" {
		t.Fatalf("down picked %q open %v, want copy-call", picked, open)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if picked, _ = app.view.Picked(); picked != "copy" {
		t.Fatalf("up picked %q, want copy back", picked)
	}

	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if typed := app.view.Value(); typed != "/copy" {
		t.Errorf("tab on two matches wrote %q, want the shared prefix /copy", typed)
	}
	typeText(app, "-")
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if typed := app.view.Value(); typed != "/copy-call" {
		t.Errorf("tab on one match wrote %q, want /copy-call", typed)
	}

	app.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if _, open := app.view.Picked(); open {
		t.Error("esc left the menu open")
	}
	if typed := app.view.Value(); typed != "/copy-call" {
		t.Errorf("esc changed the text to %q", typed)
	}
	if app.current != screenChat {
		t.Error("esc while the menu was open also switched screen")
	}
	nothingEntered(t, entered)
}

func TestTheMenuRunsTheRowThatIsPicked(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/se")
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != screenSettings {
		t.Fatalf("enter on the picked row left the app on screen %d, want settings", app.current)
	}
	nothingEntered(t, entered)
}

func TestAnUnknownSlashCommandSaysSoAndSendsNothingToTheModel(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/nope")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "there is no /nope") {
		t.Errorf("an unknown command says nothing\n%s", content)
	}
	nothingEntered(t, entered)
}

func TestOnlyALeadingSlashOnItsOwnIsACommand(t *testing.T) {
	for _, one := range []struct {
		typed string
		sent  string
	}{
		{"read internal/turn/loop.go", "read internal/turn/loop.go"},
		{"explain /internal/turn/loop.go", "explain /internal/turn/loop.go"},
		{"(/ is not a command", "(/ is not a command"},
		{"// a comment", "// a comment"},
		{"/* a block", "/* a block"},
		{`\/etc/hosts`, "/etc/hosts"},
	} {
		t.Run(one.typed, func(t *testing.T) {
			entered := make(chan string, 1)
			app := commandApp(t, entered)
			typeText(app, one.typed)
			if name, asked := app.view.Command(); asked {
				t.Fatalf("%q was read as the command %q", one.typed, name)
			}
			app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			select {
			case sent := <-entered:
				if sent != one.sent {
					t.Fatalf("the model was sent %q, want %q", sent, one.sent)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%q never reached the model", one.typed)
			}
		})
	}
}

func TestASlashOnTheSecondLineIsNotACommand(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "read this")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	typeText(app, "/settings")
	if name, asked := app.view.Command(); asked {
		t.Fatalf("a slash on the second line was read as the command %q", name)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case sent := <-entered:
		if sent != "read this\n/settings" {
			t.Fatalf("the model was sent %q", sent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the two-line task never reached the model")
	}
	if app.current != screenChat {
		t.Error("the second line opened a screen")
	}
}

func committedRepo(t *testing.T, change string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("one\n")
	git("add", "note.txt")
	git("commit", "-q", "-m", "first")
	write(change)
	return dir
}

func sentFor(t *testing.T, root, typed string) (string, string) {
	t.Helper()
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	app.options.Root = root
	typeText(app, typed)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case sent := <-entered:
		return sent, ansi.Strip(app.View().Content)
	case <-time.After(2 * time.Second):
		t.Fatalf("%q started no turn\n%s", typed, ansi.Strip(app.View().Content))
	}
	return "", ""
}

func TestTheCommandsSimplifyDebugAndHuntStartATurnCarryingTheirSkill(t *testing.T) {
	repo := committedRepo(t, "one\ntwo\n")
	for _, one := range []struct {
		typed   string
		carries []string
	}{
		{"/simplify", []string{"# Simplify", "+two"}},
		{"/debug the cursor jumps", []string{"# Debug", "the cursor jumps"}},
		{"/hunt", []string{"# Hunt", "hunter", "skeptic", "referee"}},
	} {
		t.Run(one.typed, func(t *testing.T) {
			sent, chat := sentFor(t, repo, one.typed)
			for _, want := range one.carries {
				if !strings.Contains(sent, want) {
					t.Errorf("%q sent a first message without %q:\n%s", one.typed, want, sent)
				}
			}
			if !strings.Contains(chat, one.typed) || strings.Contains(chat, one.carries[0]) {
				t.Errorf("the chat should show what was typed, not the skill\n%s", chat)
			}
		})
	}
}

func TestTheSimplifyCommandOnACleanTreeCarriesTheLastCommit(t *testing.T) {
	sent, _ := sentFor(t, committedRepo(t, "one\n"), "/simplify")
	if !strings.Contains(sent, "+one") {
		t.Errorf("a clean tree sent no diff of the last commit:\n%s", sent)
	}
}

func TestTheDebugCommandWithNoSymptomSaysHowAndKeepsTheDraft(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/debug")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if content := ansi.Strip(app.View().Content); !strings.Contains(content, "/debug <symptom>") {
		t.Errorf("/debug with no symptom does not say how\n%s", content)
	}
	if draft := app.view.Value(); draft != "/debug" {
		t.Errorf("the composer holds %q, want /debug kept to finish", draft)
	}
	nothingEntered(t, entered)
}

func TestTheHelpCommandListsEveryCommandOnALineAndSendsNothing(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	app.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
	typeText(app, "/help")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	content := ansi.Strip(app.View().Content)
	for _, want := range []string{"/simplify", "/debug <symptom>", "/hunt"} {
		if !strings.Contains(content, want) {
			t.Errorf("/help does not list %s\n%s", want, content)
		}
	}
	nothingEntered(t, entered)
}
