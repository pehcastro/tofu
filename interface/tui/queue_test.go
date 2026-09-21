package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/konst"
)

const (
	firstTask  = "rename the judge interface to decider"
	secondTask = "then update the changelog"
	thirdTask  = "and run the whole suite"
)

func queueApp(t *testing.T, tasks chan<- string) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Turn: func(_ context.Context, _, task string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			tasks <- task
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func typeAndSend(app *App, task string) {
	typeText(app, task)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func endTurn(t *testing.T, app *App) {
	t.Helper()
	for {
		msg := app.waitForEvent()()
		app.Update(msg)
		if _, done := msg.(Closed); done {
			return
		}
	}
}

func rowHolding(t *testing.T, app *App, text string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(app.View().Content), "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no row on the screen holds %q\n%s", text, ansi.Strip(app.View().Content))
	return ""
}

func TestEnterDuringATurnQueuesTheTextAndClearsTheComposer(t *testing.T) {
	tasks := make(chan string, 4)
	app := queueApp(t, tasks)
	typeAndSend(app, firstTask)
	if started := <-tasks; started != firstTask {
		t.Fatalf("the first turn ran %q", started)
	}
	typeAndSend(app, secondTask)

	if held := app.view.Value(); held != "" {
		t.Errorf("the composer still holds %q after enter during a turn", held)
	}
	if row := rowHolding(t, app, secondTask); !strings.Contains(row, "waiting") {
		t.Errorf("the queued message is not marked as waiting: %q", row)
	}
	select {
	case ran := <-tasks:
		t.Fatalf("a second turn started with %q while the first was running", ran)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestThreeMessagesTypedDuringOneTurnQueueInOrder(t *testing.T) {
	tasks := make(chan string, 4)
	app := queueApp(t, tasks)
	typeAndSend(app, firstTask)
	<-tasks
	for _, task := range []string{firstTask, secondTask, thirdTask} {
		typeAndSend(app, task)
	}
	queued := app.view.Queued()
	if len(queued) != 3 || queued[0] != firstTask || queued[1] != secondTask || queued[2] != thirdTask {
		t.Fatalf("the queue holds %q, want the three in the order they were typed", queued)
	}
	plain := ansi.Strip(app.View().Content)
	if at, next := strings.Index(plain, secondTask), strings.Index(plain, thirdTask); at > next {
		t.Errorf("the transcript shows the queue out of order\n%s", plain)
	}
}

func TestAQueuedEntryCanBeRemovedBeforeItRuns(t *testing.T) {
	tasks := make(chan string, 4)
	app := queueApp(t, tasks)
	typeAndSend(app, firstTask)
	<-tasks
	for _, task := range []string{firstTask, secondTask, thirdTask} {
		typeAndSend(app, task)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})

	queued := app.view.Queued()
	if len(queued) != 2 || queued[0] != firstTask || queued[1] != thirdTask {
		t.Fatalf("the queue holds %q, want the second one gone", queued)
	}
	if plain := ansi.Strip(app.View().Content); strings.Contains(plain, secondTask) {
		t.Errorf("the removed message is still on the screen\n%s", plain)
	}
}

func TestTheFirstQueuedMessageStartsTheNextTurn(t *testing.T) {
	tasks := make(chan string, 4)
	app := queueApp(t, tasks)
	typeAndSend(app, firstTask)
	<-tasks
	typeAndSend(app, secondTask)
	typeAndSend(app, thirdTask)
	endTurn(t, app)

	select {
	case ran := <-tasks:
		if ran != secondTask {
			t.Fatalf("the next turn was given %q, want the first queued message", ran)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the queued message never started a turn")
	}
	if !app.busy {
		t.Error("the app is not running the turn the queue started")
	}
	if queued := app.view.Queued(); len(queued) != 1 || queued[0] != thirdTask {
		t.Fatalf("the queue holds %q, want the third message alone", queued)
	}
	if row := rowHolding(t, app, secondTask); strings.Contains(row, "waiting") {
		t.Errorf("the message that ran is still marked as waiting: %q", row)
	}
	if row := rowHolding(t, app, thirdTask); !strings.Contains(row, "waiting") {
		t.Errorf("the message still queued lost its mark: %q", row)
	}
}

func TestEnterWithNothingRunningQueuesNothing(t *testing.T) {
	tasks := make(chan string, 4)
	app := queueApp(t, tasks)
	typeAndSend(app, firstTask)
	if started := <-tasks; started != firstTask {
		t.Fatalf("the turn ran %q", started)
	}
	if queued := app.view.Queued(); len(queued) != 0 {
		t.Fatalf("a send with nothing running queued %q", queued)
	}
	if row := rowHolding(t, app, firstTask); strings.Contains(row, "waiting") {
		t.Errorf("the message that ran at once is marked as waiting: %q", row)
	}
}

func TestStoppingTheTurnDropsTheQueue(t *testing.T) {
	tasks := make(chan string, 4)
	app := queueApp(t, tasks)
	typeAndSend(app, firstTask)
	<-tasks
	typeAndSend(app, secondTask)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if queued := app.view.Queued(); len(queued) != 0 {
		t.Fatalf("ctrl+c left %q in the queue", queued)
	}
	if plain := ansi.Strip(app.View().Content); !strings.Contains(plain, stoppingNote+droppedQueue) {
		t.Errorf("the transcript does not say the queue was dropped\n%s", plain)
	}
}

func TestNoGoldenFixtureCarriesTheVersion(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("testdata", "*.golden"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no golden fixture was read: %v", err)
	}
	for _, name := range names {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), konst.Version) {
			t.Errorf("%s carries the version %s, so every release moves it", name, konst.Version)
		}
	}
}

func TestNoGoldenFixtureCarriesAVendorToolUseID(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("testdata", "*.golden"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no golden fixture was read: %v", err)
	}
	for _, name := range names {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "toolu_") {
			t.Errorf("%s carries a vendor tool-use id on screen", name)
		}
	}
}
