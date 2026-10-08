package host

import (
	"context"
	"slices"
	"testing"
	"time"

	"tofu/internal/cron"
	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
)

func TestATurnSaysWhetherThePersonOrACronJobStartedIt(t *testing.T) {
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	origins, release := make(chan Origin, 3), make(chan struct{})
	h, _ := New(Config{Now: func() time.Time { return at }, Play: func(ctx context.Context, _ Pick, _ string, live Live) {
		origins <- live.Origin
		select {
		case <-release:
		case <-ctx.Done():
		}
	}})
	t.Cleanup(h.Close)
	command := func(line string) {
		if _, err := h.CronCommand(line); err != nil {
			t.Fatal(err)
		}
	}
	idle := func() {
		for _, running := h.Turn(); running; _, running = h.Turn() {
			time.Sleep(time.Millisecond)
		}
	}
	if !h.Send(Pick{Fired: "c9"}, "fix the build") {
		t.Fatal("the typed turn did not start")
	}
	typed := <-origins
	release <- struct{}{}
	idle()
	command("/loop 1m check the build")
	h.fire(h.cron.Due(context.Background(), at.Add(2*time.Minute), cron.Tick))
	fired := <-origins
	command("/loop 1m check the docs")
	h.fire(h.cron.Due(context.Background(), at.Add(4*time.Minute), cron.Tick))
	command("/cron delete c2")
	release <- struct{}{}
	unread := <-origins
	release <- struct{}{}
	idle()

	want := []Origin{{Kind: OriginPerson}, {Kind: OriginCron, Job: "c1", Schedule: "every 1m"}, {Kind: OriginCron, Job: "c2"}}
	if got := []Origin{typed, fired, unread}; !slices.Equal(got, want) {
		t.Errorf("the three turns were started by %+v, want %+v", got, want)
	}
	var started []Origin
	for len(h.events) > 0 {
		if event := <-h.events; event.Kind == EventTurnStarted {
			started = append(started, event.Origin)
		}
	}
	if !slices.Equal(started, want) {
		t.Errorf("turn.started said %+v, want %+v", started, want)
	}
}

func TestAReopenedSessionSaysWhoSentEachMessage(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	at, id := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC), session.NewEventID()
	env := "<env>working directory: " + project + "</env>\n\n"
	cronJob := Origin{Kind: OriginCron, Job: "c3", Schedule: "0 9 * * *"}
	sent := []struct {
		text   string
		source string
		want   Origin
	}{
		{"fix the build", "task", Origin{Kind: OriginPerson}},
		{"cron c3 fired, not typed by the person:\ncheck the build", cronJob.recorded(), cronJob},
		{"an old session wrote no origin", "", Origin{Kind: OriginPerson}},
		{"the stop hook says the tests fail", "stop hook", Origin{Kind: OriginTofu, Source: "stop hook"}},
		{"sub-agent research-9 is finished, the shaders compile", "sub-agent report", Origin{Kind: OriginAgent, Name: "research-9"}},
		{"and keep it short", "sub-agent check and typed by the person", Origin{Kind: OriginPerson}},
	}
	events := []recordedEvent{reported("research-9", roster.Finished, sent[4].text, at)}
	carry := Carry{Session: id}
	for _, one := range sent {
		events = append(events, recordedEvent{session.Event{At: at, Kind: session.EventMessage}, session.MessageBody{Role: session.RoleUser, Content: env + one.text, Origin: one.source}})
		carry.Messages = append(carry.Messages, llm.Message{Role: llm.RoleUser, Content: env + one.text})
	}
	recordSession(t, store, session.Header{ID: id, At: at}, events...)

	var got, want []Origin
	for _, event := range resumedChat(carry, project) {
		if event.Kind == EventTask {
			got = append(got, event.Origin)
		}
	}
	for _, one := range sent {
		want = append(want, one.want)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the reopened messages say %+v, want %+v", got, want)
	}
}
