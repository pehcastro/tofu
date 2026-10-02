package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/subagent"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

const steerBuffer = 8

func steeringTurn(steering chan string, started, taken chan<- string, release <-chan struct{}) Turn {
	return func(_ context.Context, _ Pick, task string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
		started <- task
		<-release
		for {
			select {
			case steered := <-steering:
				taken <- steered
				emit(Event{Kind: EventSteered, Text: steered})
			default:
				return
			}
		}
	}
}

func steerApp(t *testing.T, steering chan string, turn Turn) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:     testRepo,
		Branch:   "develop",
		Now:      fixedClock(),
		Wires:    anthropicAlone,
		Turn:     turn,
		Steering: steering,
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func TestAQueuedRowIsUnmarkedWhenTheModelTakesItAndNotWhenItWasQueued(t *testing.T) {
	steering := make(chan string, steerBuffer)
	started, release := make(chan string, 2), make(chan struct{})
	app := steerApp(t, steering, steeringTurn(steering, started, make(chan string, 2), release))
	typeAndSend(app, firstTask)
	<-started
	typeAndSend(app, secondTask)

	if row := headerOf(t, app, secondTask); !strings.Contains(row, "waiting") {
		t.Fatalf("the queued row lost its mark before the model took it: %q", row)
	}
	if len(app.view.Queued()) != 1 {
		t.Fatalf("the app stopped tracking the queued message the moment it handed it over: %q", app.view.Queued())
	}

	close(release)
	for delivered := false; !delivered; {
		msg := app.waitForEvent()()
		app.Update(msg)
		if _, done := msg.(Closed); done {
			t.Fatal("the turn ended without the queued message ever reaching the model")
		}
		event, sent := msg.(Event)
		delivered = sent && event.Kind == EventSteered
	}
	if row := headerOf(t, app, secondTask); strings.Contains(row, "waiting") {
		t.Errorf("the row is still marked waiting after the model took the message: %q", row)
	}
	if queued := app.view.Queued(); len(queued) != 0 {
		t.Errorf("the queue still holds %q after the message was delivered", queued)
	}
}

func TestAMessageQueuedAfterTheLastStepStartsTheNextTurn(t *testing.T) {
	steering := make(chan string, steerBuffer)
	started := make(chan string, 2)
	ended := make(chan struct{})
	app := steerApp(t, steering, func(_ context.Context, _ Pick, task string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
		started <- task
		<-ended
	})
	typeAndSend(app, firstTask)
	<-started
	typeAndSend(app, secondTask)
	close(ended)
	endTurn(t, app)

	select {
	case next := <-started:
		if next != secondTask {
			t.Fatalf("the next turn was given %q, want the message nobody drained", next)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a message queued after the last step was dropped instead of starting the next turn")
	}
	if held := len(steering); held != 0 {
		t.Errorf("%d messages are still in the steering channel, so the next turn would be given one twice", held)
	}
}

func TestStoppingTheTurnEmptiesTheSteeringChannelToo(t *testing.T) {
	steering := make(chan string, steerBuffer)
	started, release := make(chan string, 2), make(chan struct{})
	defer close(release)
	app := steerApp(t, steering, steeringTurn(steering, started, make(chan string, 2), release))
	typeAndSend(app, firstTask)
	<-started
	typeAndSend(app, secondTask)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if held := len(steering); held != 0 {
		t.Errorf("ctrl+c left %d messages in the steering channel", held)
	}
}

func TestALongQueuedMessageIsCutOnScreenAndReachesTheModelWhole(t *testing.T) {
	steering := make(chan string, steerBuffer)
	started, taken, release := make(chan string, 2), make(chan string, 2), make(chan struct{})
	app := steerApp(t, steering, steeringTurn(steering, started, taken, release))
	long := strings.Repeat("read the policy before the wire, ", 8) + "and say so"
	typeAndSend(app, firstTask)
	<-started
	typeAndSend(app, long)

	row := rowHolding(t, app, "read the policy before the wire")
	if !strings.Contains(row, "…") {
		t.Errorf("the queued row is not cut, so a long message reflows the transcript: %q", row)
	}
	if strings.Contains(row, "and say so") {
		t.Errorf("the queued row shows the whole message: %q", row)
	}
	if width := widget.Cells(ansi.Strip(row)); width > 80 {
		t.Errorf("the queued row is %d columns wide on an 80 column screen", width)
	}

	close(release)
	select {
	case delivered := <-taken:
		if delivered != long {
			t.Fatalf("the model was given %q, want the message whole", delivered)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the long message never reached the model")
	}
}

func pumpUntil(t *testing.T, app *App, kind EventKind) {
	t.Helper()
	for {
		msg := app.waitForEvent()()
		app.Update(msg)
		if _, done := msg.(Closed); done {
			t.Fatalf("the turn closed before an event of kind %d arrived", kind)
		}
		if event, sent := msg.(Event); sent && event.Kind == kind {
			return
		}
	}
}

func subAgentLoopApp(t *testing.T, steering chan string, stops chan struct{}, turn Turn) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone, Turn: turn, Steering: steering, StopLead: stops})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func runningSubAgent() []subagent.Row {
	return []subagent.Row{{Name: "ts-dev-1", State: roster.Working}}
}

func TestEscAndCtrlCWhileOnlyASubAgentRunsLeaveItRunning(t *testing.T) {
	stops, alive, release := make(chan struct{}, 1), make(chan bool, 1), make(chan struct{})
	app := subAgentLoopApp(t, make(chan string, steerBuffer), stops, func(ctx context.Context, _ Pick, _ string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
		emit(Event{Kind: EventRequesting})
		emit(Event{Kind: EventText, Text: "ts-dev-1 is on the routes"})
		emit(Event{Kind: EventDone, Text: "finished in", SubAgents: runningSubAgent()})
		<-release
		alive <- ctx.Err() == nil
	})
	typeAndSend(app, firstTask)
	pumpUntil(t, app, EventDone)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	close(release)

	if !<-alive {
		t.Error("esc and one ctrl+c with the lead idle cancelled the loop, and the sub-agent with it")
	}
	if len(stops) != 0 {
		t.Error("a stop was sent to a lead that runs no turn")
	}
	endTurn(t, app)
}

func TestEscMidLeadTurnStopsTheLeadAndTheSubAgentKeepsRunning(t *testing.T) {
	steering := make(chan string, steerBuffer)
	stops, alive, told, release := make(chan struct{}, 1), make(chan bool, 1), make(chan bool, 1), make(chan struct{})
	app := subAgentLoopApp(t, steering, stops, func(ctx context.Context, _ Pick, _ string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
		emit(Event{Kind: EventRequesting})
		emit(Event{Kind: EventDone, Text: "finished in", SubAgents: runningSubAgent()})
		emit(Event{Kind: EventRequesting})
		emit(Event{Kind: EventText, Text: "the lead keeps going"})
		select {
		case <-stops:
			told <- true
		case <-time.After(2 * time.Second):
			told <- false
		}
		emit(Event{Kind: EventDone, Text: "cancelled at", SubAgents: runningSubAgent()})
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		alive <- ctx.Err() == nil
	})
	typeAndSend(app, firstTask)
	pumpUntil(t, app, EventText)
	typeAndSend(app, secondTask)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	pumpUntil(t, app, EventDone)

	if !<-told {
		t.Error("esc mid lead turn never told the lead to stop")
	}
	if len(steering) != 1 || len(app.view.Queued()) != 1 {
		t.Errorf("the stop dropped the message typed before it: %d steered, queue %q", len(steering), app.view.Queued())
	}
	close(release)
	if !<-alive {
		t.Error("esc mid lead turn cancelled the loop, and the sub-agent with it")
	}
	endTurn(t, app)
}

func TestAMessageTypedWhileOnlySubAgentsRunStartsALeadTurnAndIsNeverDrawnWaiting(t *testing.T) {
	steering := make(chan string, steerBuffer)
	release := make(chan struct{})
	app := steerApp(t, steering, func(_ context.Context, _ Pick, _ string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
		emit(Event{Kind: EventRequesting})
		emit(Event{Kind: EventText, Text: "ts-dev-1 is on the routes"})
		emit(Event{Kind: EventDone, Text: "finished in", SubAgents: []subagent.Row{{Name: "ts-dev-1", State: roster.Working}}})
		typed := <-steering
		emit(Event{Kind: EventSteered, Text: typed})
		emit(Event{Kind: EventRequesting})
		emit(Event{Kind: EventText, Text: "the lead answers while ts-dev-1 works"})
		<-release
	})
	typeAndSend(app, firstTask)
	pumpUntil(t, app, EventDone)
	typeAndSend(app, secondTask)

	if row := headerOf(t, app, secondTask); strings.Contains(row, "waiting") {
		t.Fatalf("a message typed while only a sub-agent runs is drawn waiting: %q", row)
	}
	if queued := app.view.Queued(); len(queued) != 0 {
		t.Fatalf("a message typed while the lead is idle sits in the queue: %q", queued)
	}
	pumpUntil(t, app, EventText)
	if row := headerOf(t, app, secondTask); strings.Contains(row, "waiting") {
		t.Errorf("the message is drawn waiting after the lead took it: %q", row)
	}
	if row := turnRow(t, app); !strings.Contains(row, "thinking") && !strings.Contains(row, "requesting") {
		t.Errorf("the lead turn the message started does not run on the status line: %q", row)
	}
	rows := plainRows(app.View())
	answer := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "the lead answers") })
	asked := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, secondTask) })
	if answer < 0 || asked < 0 || answer < asked {
		t.Errorf("the lead's answer is not drawn under the message it answers\n%s", strings.Join(rows, "\n"))
	}
	close(release)
	endTurn(t, app)
}
