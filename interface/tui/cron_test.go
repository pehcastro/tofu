package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/cron"
)

func TestAFiredJobStartsATurnWhenIdleAndSteersWhenBusy(t *testing.T) {
	steering, picks, release := make(chan string, steerBuffer), make(chan Pick, 2), make(chan struct{})
	book := &cron.Book{}
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone, Steering: steering, Cron: book,
		Turn: func(_ context.Context, pick Pick, _ string, emit CalledFromInsideTheTurnAndNeverAfterItReturns) {
			picks <- pick
			<-release
			emit(Event{Kind: EventText, Text: "the build passes"})
		}})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	typeAndSend(app, "/loop 1m check the build")
	typeAndSend(app, "/loop 2m check the docs")
	if app.busy || app.status.Crons != 2 {
		t.Fatalf("making two loops started a turn (%v) or the status counts %d jobs, want 2", app.busy, app.status.Crons)
	}
	typeAndSend(app, "/cron")
	if _, open := app.top().(cronDialog); !open || !strings.Contains(ansi.Strip(app.View().Content), "cron c2 · every 2m") {
		t.Fatalf("/cron drew no list of the jobs:\n%s", ansi.Strip(app.View().Content))
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	app.Update(cronTickMsg(book.Due(context.Background(), fixedClock()().Add(3*time.Minute), cron.Tick)))
	if pick := <-picks; pick.Fired != "c1" {
		t.Errorf("the first fire started a turn marked %q, want c1, so the push guard never knows", pick.Fired)
	}
	select {
	case steered := <-steering:
		if !strings.Contains(steered, "cron c2 fired") {
			t.Errorf("the second fire steered %q, want c2's prompt", steered)
		}
	case pick := <-picks:
		t.Errorf("the second fire started a second turn, marked %q", pick.Fired)
	}
	close(release)
	var closing tea.Cmd
	for closing == nil {
		msg := app.waitForEvent()()
		_, cmd := app.Update(msg)
		if _, closed := msg.(Closed); closed {
			closing = cmd
		}
	}
	if job, _ := book.Job("c1"); job.LastResult != "the build passes" {
		t.Errorf("c1 ended its turn with last result %q, want the answer, and it stays waiting", job.LastResult)
	}
	if job, _ := book.Job("c2"); job.LastResult != "" {
		t.Errorf("c2's prompt was never read and the turn's answer was booked to it: %q", job.LastResult)
	}
	refired := quickMessages(closing)
	if len(refired) != 1 || len(refired[0]) != 1 || refired[0][0].ID != "c2" {
		t.Fatalf("the unread fire was not posted again when the turn closed: %+v", refired)
	}
	app.Update(refired[0])
	if pick := <-picks; pick.Fired != "c2" {
		t.Errorf("the unread fire started a turn marked %q, want c2", pick.Fired)
	}
}

func quickMessages(cmd tea.Cmd) []cronFiresMsg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case tea.BatchMsg:
			var found []cronFiresMsg
			for _, inner := range msg {
				found = append(found, quickMessages(inner)...)
			}
			return found
		case cronFiresMsg:
			if len(msg) > 0 {
				return []cronFiresMsg{msg}
			}
		}
	case <-time.After(cron.PollMillis * time.Millisecond / 4):
	}
	return nil
}
