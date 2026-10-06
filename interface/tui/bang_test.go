package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	isession "tofu/internal/session"
)

const bangSettle = 2 * time.Second

func bangApp(t *testing.T, history string, ran, tasks chan string, run func(ctx context.Context) (string, bool)) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:          testRepo,
		Now:           fixedClock(),
		Wires:         anthropicAlone,
		PromptHistory: history,
		RunCommand: func(ctx context.Context, command string) (string, bool) {
			ran <- command
			return run(ctx)
		},
		Turn: func(ctx context.Context, _ Pick, task string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			tasks <- task
			<-ctx.Done()
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func printed(output string) func(context.Context) (string, bool) {
	return func(context.Context) (string, bool) { return output, false }
}

func commandEnded(cmd tea.Cmd) <-chan tea.Msg {
	found := make(chan tea.Msg, 1)
	var walk func(cmd tea.Cmd)
	walk = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case msg := <-done:
			if batch, isBatch := msg.(tea.BatchMsg); isBatch {
				for _, inner := range batch {
					go walk(inner)
				}
				return
			}
			if _, ended := msg.(ranMsg); ended {
				found <- msg
			}
		case <-time.After(bangSettle):
		}
	}
	go walk(cmd)
	return found
}

func sendBang(app *App, typed string) <-chan tea.Msg {
	typeText(app, typed)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return commandEnded(cmd)
}

func took(t *testing.T, from chan string) string {
	t.Helper()
	select {
	case got := <-from:
		return got
	case <-time.After(bangSettle):
		t.Fatal("nothing arrived")
		return ""
	}
}

func awaitEnd(t *testing.T, app *App, ended <-chan tea.Msg) {
	t.Helper()
	select {
	case msg := <-ended:
		app.Update(msg)
	case <-time.After(bangSettle):
		t.Fatalf("the command never ended\n%s", screenText(app))
	}
}

func TestBangRunsTheCommandWithoutATurnAndDrawsItsOutput(t *testing.T) {
	ran, tasks := make(chan string, 2), make(chan string, 2)
	app := bangApp(t, t.TempDir(), ran, tasks, printed("On branch main\nnothing to commit, working tree clean\n"))
	ended := sendBang(app, "!git status")
	if got := took(t, ran); got != "git status" {
		t.Fatalf("the shell was handed %q, want git status", got)
	}
	awaitEnd(t, app, ended)
	shown := screenText(app)
	for _, want := range []string{"!git status", "On branch main", "working tree clean"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the chat never shows %q\n%s", want, shown)
		}
	}
	if len(tasks) != 0 || app.busy {
		t.Fatalf("a ! command started a turn: %d tasks, busy %v", len(tasks), app.busy)
	}
}

func TestBangAloneOrWithOnlySpacesRunsNothingAndSendsNothing(t *testing.T) {
	for _, typed := range []string{"!", "!    "} {
		ran, tasks := make(chan string, 2), make(chan string, 2)
		app := bangApp(t, t.TempDir(), ran, tasks, printed("ran"))
		sendBang(app, typed)
		if len(ran) != 0 || len(tasks) != 0 || app.busy {
			t.Fatalf("%q ran %d commands and sent %d tasks", typed, len(ran), len(tasks))
		}
	}
}

func TestBangWhileATurnRunsIsRefusedAndStaysInTheComposer(t *testing.T) {
	ran, tasks := make(chan string, 2), make(chan string, 2)
	app := bangApp(t, t.TempDir(), ran, tasks, printed("ran"))
	typeAndSend(app, "rename the ledger writer")
	took(t, tasks)
	sendBang(app, "!git status")
	if len(ran) != 0 {
		t.Fatalf("a ! command ran while a turn was running")
	}
	if shown := screenText(app); !strings.Contains(shown, "turn") || app.view.Value() != "!git status" {
		t.Fatalf("the refusal is not drawn or the composer lost the command (%q)\n%s", app.view.Value(), shown)
	}
}

func TestAPromptWhileACommandRunsIsRefused(t *testing.T) {
	ran, tasks := make(chan string, 2), make(chan string, 2)
	release := make(chan struct{})
	app := bangApp(t, t.TempDir(), ran, tasks, func(context.Context) (string, bool) { <-release; return "done", false })
	ended := sendBang(app, "!git log")
	took(t, ran)
	typeAndSend(app, "what changed")
	sendBang(app, "!git status")
	close(release)
	awaitEnd(t, app, ended)
	if len(tasks) != 0 || len(ran) != 0 {
		t.Fatalf("while a command ran, %d prompts were sent and %d more commands ran", len(tasks), len(ran))
	}
	if shown := screenText(app); !strings.Contains(shown, "esc stops it") {
		t.Fatalf("the refusal never says how to stop the command\n%s", shown)
	}
}

func TestEscStopsTheRunningCommandAndSaysSo(t *testing.T) {
	ran, tasks := make(chan string, 2), make(chan string, 2)
	app := bangApp(t, t.TempDir(), ran, tasks, func(ctx context.Context) (string, bool) { <-ctx.Done(); return "64 bytes from 127.0.0.1", true })
	ended := sendBang(app, "!ping -t 127.0.0.1")
	took(t, ran)
	typeText(app, "and now")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	awaitEnd(t, app, ended)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	shown := screenText(app)
	if !strings.Contains(shown, "64 bytes from") || !strings.Contains(shown, "stopped") {
		t.Fatalf("esc did not stop the command, or the chat does not say so\n%s", shown)
	}
	if got := took(t, tasks); got != "and now" {
		t.Fatalf("the prompt after a stopped command sent %q", got)
	}
}

func TestABangPromptPickedFromHistoryRunsAsACommandEvenAsAChip(t *testing.T) {
	dir := t.TempDir()
	long := "!" + strings.Repeat("echo the gate reads the policy && ", 10) + "echo done"
	if err := isession.OpenPromptHistory(dir).Add(long); err != nil {
		t.Fatal(err)
	}
	ran, tasks := make(chan string, 2), make(chan string, 2)
	app := bangApp(t, dir, ran, tasks, printed("done"))
	pressCtrlP(app)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.HasPrefix(app.view.Value(), "[Text ") {
		t.Fatalf("the long command did not come back as a chip: %q", app.view.Value())
	}
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	awaitEnd(t, app, commandEnded(cmd))
	if got := took(t, ran); got != strings.TrimPrefix(long, "!") || len(tasks) != 0 {
		t.Fatalf("the recalled command ran as %q and %d prompts went to the model", got, len(tasks))
	}
}

func TestTheHistoryKeepsABangCommandAsTyped(t *testing.T) {
	dir := t.TempDir()
	ran, tasks := make(chan string, 2), make(chan string, 2)
	app := bangApp(t, dir, ran, tasks, printed("On branch main"))
	awaitEnd(t, app, sendBang(app, "!git status"))
	if kept := isession.OpenPromptHistory(dir).Prompts(); len(kept) == 0 || kept[0] != "!git status" {
		t.Fatalf("the history does not keep the ! command as typed: %q", kept)
	}
}
