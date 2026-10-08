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
	"tofu/internal/host"
	roster "tofu/internal/subagent"
	"tofu/internal/widget"
)

type played struct {
	task string
	live host.Live
}

func steeringTurn(started chan<- played, taken chan<- string, release <-chan struct{}) host.Play {
	return func(_ context.Context, _ Pick, task string, live host.Live) {
		started <- played{task: task, live: live}
		<-release
		for {
			select {
			case steered := <-live.Steering:
				taken <- steered
				live.Emit(Event{Kind: EventSteered, Text: steered})
			default:
				return
			}
		}
	}
}

func steerApp(t *testing.T, play host.Play) *App {
	t.Helper()
	playing, _ := host.New(host.Config{Now: fixedClock(), Play: play})
	t.Cleanup(playing.Close)
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Host:   playing,
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func TestThreeMessagesTakenInOneStepBecomeOneSentMessageUnderTheWorkBeforeIt(t *testing.T) {
	release := make(chan struct{})
	app := steerApp(t, func(_ context.Context, _ Pick, _ string, live host.Live) {
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "working on the first ask"})
		<-release
		for drained := false; !drained; {
			select {
			case steered := <-live.Steering:
				live.Emit(Event{Kind: EventSteered, Text: steered})
			default:
				drained = true
			}
		}
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "read all three"})
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	typeAndSend(app, "start the work")
	pumpUntil(t, app, EventText)
	for _, task := range []string{firstTask, secondTask, thirdTask} {
		typeAndSend(app, task)
	}
	if row := headerOf(t, app, firstTask); !strings.Contains(row, "queued 3") {
		t.Fatalf("three queued messages are not drawn under one queue head: %q\n%s", row, ansi.Strip(app.View().Content))
	}
	if headers := youHeaders(app); headers != 1 {
		t.Fatalf("the queue drew %d messages from the person before the lead read any\n%s", headers, ansi.Strip(app.View().Content))
	}

	close(release)
	pumpUntil(t, app, EventText)
	if queued := app.view.Queued(); len(queued) != 0 {
		t.Errorf("the queue still holds %q after the lead took it", queued)
	}
	if headers := youHeaders(app); headers != 2 {
		t.Errorf("the three taken together are drawn as %d messages, want one beside the first\n%s", headers-1, ansi.Strip(app.View().Content))
	}
	rows := plainRows(app.View())
	at := func(text string) int {
		return slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, text) })
	}
	if order := []int{at("working on the first"), at(firstTask), at(secondTask), at(thirdTask), at("read all three")}; slices.Contains(order, -1) || !slices.IsSorted(order) {
		t.Errorf("want the work, then the three in order, then the answer; rows %v\n%s", order, strings.Join(rows, "\n"))
	}
	if plain := ansi.Strip(app.View().Content); strings.Contains(plain, "queued") {
		t.Errorf("the queue head is still drawn after the lead took everything\n%s", plain)
	}
	endTurn(t, app)
}

func TestCtrlXTakesAQueuedMessageBackFromTheLead(t *testing.T) {
	started, taken, release := make(chan played, 2), make(chan string, 4), make(chan struct{})
	app := steerApp(t, steeringTurn(started, taken, release))
	typeAndSend(app, "start the work")
	<-started
	for _, task := range []string{firstTask, secondTask, thirdTask} {
		typeAndSend(app, task)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	close(release)
	endTurn(t, app)
	close(taken)
	var read []string
	for steered := range taken {
		read = append(read, steered)
	}
	if !slices.Equal(read, []string{firstTask, thirdTask}) {
		t.Fatalf("after ctrl+x on the second the lead read %q", read)
	}
}

func TestAMessageQueuedAfterTheLastStepStartsTheNextTurn(t *testing.T) {
	started := make(chan played, 2)
	ended := make(chan struct{})
	app := steerApp(t, func(_ context.Context, _ Pick, task string, live host.Live) {
		started <- played{task: task, live: live}
		<-ended
	})
	typeAndSend(app, firstTask)
	first := <-started
	typeAndSend(app, secondTask)
	close(ended)
	endTurn(t, app)

	select {
	case next := <-started:
		if next.task != secondTask {
			t.Fatalf("the next turn was given %q, want the message nobody drained", next.task)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a message queued after the last step was dropped instead of starting the next turn")
	}
	if held := len(first.live.Steering); held != 0 {
		t.Errorf("%d messages are still in the steering channel, so the next turn would be given one twice", held)
	}
}

func TestStoppingTheTurnEmptiesTheSteeringChannelToo(t *testing.T) {
	started, release := make(chan played, 2), make(chan struct{})
	defer close(release)
	app := steerApp(t, steeringTurn(started, make(chan string, 2), release))
	typeAndSend(app, firstTask)
	first := <-started
	typeAndSend(app, secondTask)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if held := len(first.live.Steering); held != 0 {
		t.Errorf("ctrl+c left %d messages in the steering channel", held)
	}
}

func TestALongQueuedMessageIsCutOnScreenAndReachesTheModelWhole(t *testing.T) {
	started, taken, release := make(chan played, 2), make(chan string, 2), make(chan struct{})
	app := steerApp(t, steeringTurn(started, taken, release))
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

func runningSubAgent() []subagent.Row {
	return []subagent.Row{{Name: "ts-dev-1", State: roster.Working}}
}

func TestEscAndCtrlCWhileOnlyASubAgentRunsLeaveItRunning(t *testing.T) {
	alive, release, stops := make(chan bool, 1), make(chan struct{}), make(chan (<-chan struct{}), 1)
	app := steerApp(t, func(ctx context.Context, _ Pick, _ string, live host.Live) {
		stops <- live.LeadStop
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "ts-dev-1 is on the routes"})
		live.Emit(Event{Kind: EventDone, Text: "finished in", SubAgents: runningSubAgent()})
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
	if len(<-stops) != 0 {
		t.Error("a stop was sent to a lead that runs no turn")
	}
	endTurn(t, app)
}

func TestEscMidLeadTurnStopsTheLeadAndTheSubAgentKeepsRunning(t *testing.T) {
	alive, told, release, steering := make(chan bool, 1), make(chan bool, 1), make(chan struct{}), make(chan (<-chan string), 1)
	app := steerApp(t, func(ctx context.Context, _ Pick, _ string, live host.Live) {
		steering <- live.Steering
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventDone, Text: "finished in", SubAgents: runningSubAgent()})
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "the lead keeps going"})
		select {
		case <-live.LeadStop:
			told <- true
		case <-time.After(2 * time.Second):
			told <- false
		}
		live.Emit(Event{Kind: EventDone, Text: "cancelled at", SubAgents: runningSubAgent()})
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
	if held := len(<-steering); held != 1 || len(app.view.Queued()) != 1 {
		t.Errorf("the stop dropped the message typed before it: %d steered, queue %q", held, app.view.Queued())
	}
	close(release)
	if !<-alive {
		t.Error("esc mid lead turn cancelled the loop, and the sub-agent with it")
	}
	endTurn(t, app)
}

func TestAMessageTypedWhileOnlySubAgentsRunIsQueuedUntilTheLeadTakesIt(t *testing.T) {
	release := make(chan struct{})
	app := steerApp(t, func(_ context.Context, _ Pick, _ string, live host.Live) {
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "ts-dev-1 is on the routes"})
		live.Emit(Event{Kind: EventDone, Text: "finished in", SubAgents: []subagent.Row{{Name: "ts-dev-1", State: roster.Working}}})
		typed := <-live.Steering
		live.Emit(Event{Kind: EventSteered, Text: typed})
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "the lead answers while ts-dev-1 works"})
		<-release
	})
	typeAndSend(app, firstTask)
	pumpUntil(t, app, EventDone)
	typeAndSend(app, secondTask)

	if row := headerOf(t, app, secondTask); !strings.Contains(row, "queued 1") {
		t.Fatalf("a message the lead has not taken yet is drawn as sent: %q", row)
	}
	pumpUntil(t, app, EventText)
	if queued := app.view.Queued(); len(queued) != 0 {
		t.Fatalf("the message sits in the queue after the lead took it: %q", queued)
	}
	if row := headerOf(t, app, secondTask); !strings.HasPrefix(strings.TrimSpace(row), "You") {
		t.Errorf("the message is not drawn as sent after the lead took it: %q", row)
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
