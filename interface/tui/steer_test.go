package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/widget"
)

const steerBuffer = 8

func steeringTurn(steering chan string, started, taken chan<- string, release <-chan struct{}) Turn {
	return func(_ context.Context, _, task string, emit func(Event)) {
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
		Repo:     "silo",
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

	if row := rowHolding(t, app, secondTask); !strings.Contains(row, "waiting") {
		t.Fatalf("the queued row lost its mark before the model took it: %q", row)
	}
	if len(app.view.Queued()) != 1 {
		t.Fatalf("the app stopped tracking the queued message the moment it handed it over: %q", app.view.Queued())
	}

	close(release)
	for delivered := false; !delivered; {
		msg := app.waitForEvent()()
		app.Update(msg)
		if _, done := msg.(closedMsg); done {
			t.Fatal("the turn ended without the queued message ever reaching the model")
		}
		event, sent := msg.(Event)
		delivered = sent && event.Kind == EventSteered
	}
	if row := rowHolding(t, app, secondTask); strings.Contains(row, "waiting") {
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
	app := steerApp(t, steering, func(_ context.Context, _, task string, _ func(Event)) {
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
