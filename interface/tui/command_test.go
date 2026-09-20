package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func commandApp(t *testing.T, entered chan<- string) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:       "silo",
		Branch:     "develop",
		Now:        fixedClock(),
		Wires:      anthropicAlone,
		Copy:       func(string) error { return nil },
		Paths:      repoPaths,
		ResumeHead: func() string { return "continuing turn-19a2b3c4d5, 12 messages from 3 steps" },
		NewSession: func() string { return "the next task starts a new session" },
		Turn: func(_ context.Context, _, task string, _ func(Event)) {
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

	if app.current != viewSettings {
		t.Fatalf("/settings left the app on view %d, want the settings view", app.current)
	}
	nothingEntered(t, entered)
	if left := app.view.Value(); left != "" {
		t.Errorf("the composer still holds %q after the command ran", left)
	}
	content := ansi.Strip(app.View().Content)
	if strings.Contains(content, "what should boji do here?") {
		t.Errorf("the settings view still draws the composer\n%s", content)
	}
}

func TestASlashListsTheCommandsAndTypingFiltersTheList(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/")
	listed := ansi.Strip(app.View().Content)
	for _, want := range []string{"/session", "/crew", "/settings", "/quit"} {
		if !strings.Contains(listed, want) {
			t.Errorf("a bare slash does not list %s\n%s", want, listed)
		}
	}

	typeText(app, "se")
	filtered := ansi.Strip(app.View().Content)
	for _, want := range []string{"/session", "/settings"} {
		if !strings.Contains(filtered, want) {
			t.Errorf("/se does not list %s\n%s", want, filtered)
		}
	}
	for _, gone := range []string{"/crew", "/quit"} {
		if strings.Contains(filtered, gone) {
			t.Errorf("/se still lists %s\n%s", gone, filtered)
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
			nothingEntered(t, entered)
		})
	}
}

func TestSlashResumeSaysWhatItTookAndSlashNewIsRefusedWhileATurnRuns(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/resume")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if content := ansi.Strip(app.View().Content); !strings.Contains(content, "continuing turn-19a2b3c4d5") {
		t.Errorf("/resume does not say what it took\n%s", content)
	}
	nothingEntered(t, entered)

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

func TestTheOpenMenuGolden(t *testing.T) {
	app := commandApp(t, make(chan string, 1))
	typeText(app, "/se")
	assertGolden(t, "session-menu-80x24.golden", app.View().Content)
}

func TestTheMenuMovesCompletesAndCloses(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/se")
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	picked, open := app.view.Picked()
	if !open || picked != "settings" {
		t.Fatalf("down picked %q open %v, want settings", picked, open)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if picked, _ = app.view.Picked(); picked != "session" {
		t.Fatalf("up picked %q, want session back", picked)
	}

	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if typed := app.view.Value(); typed != "/se" {
		t.Errorf("tab on two matches wrote %q, want the shared prefix /se", typed)
	}
	typeText(app, "t")
	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if typed := app.view.Value(); typed != "/settings" {
		t.Errorf("tab on one match wrote %q, want /settings", typed)
	}

	app.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if _, open := app.view.Picked(); open {
		t.Error("esc left the menu open")
	}
	if typed := app.view.Value(); typed != "/settings" {
		t.Errorf("esc changed the text to %q", typed)
	}
	if app.current != viewSession {
		t.Error("esc while the menu was open also switched view")
	}
	nothingEntered(t, entered)
}

func TestTheMenuRunsTheRowThatIsPicked(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/se")
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != viewSettings {
		t.Fatalf("enter on the picked row left the app on view %d, want settings", app.current)
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
	if app.current != viewSession {
		t.Error("the second line opened a view")
	}
}
