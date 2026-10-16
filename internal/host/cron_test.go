package host

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/cron"
)

func TestAFiredJobStartsATurnWhenIdleAndWaitsInTheLeadsInboxWhenBusy(t *testing.T) {
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	picks, release := make(chan Pick, 2), make(chan struct{})
	h, _ := New(Config{Now: func() time.Time { return at }, Play: func(ctx context.Context, pick Pick, _ string, live Live) {
		picks <- pick
		select {
		case <-release:
			live.Emit(Event{Kind: EventText, Text: "the build passes"})
		case <-ctx.Done():
		}
	}})
	t.Cleanup(h.Close)
	for _, line := range []string{"/loop 1m check the build", "/loop 2m check the docs"} {
		if _, err := h.CronCommand(line); err != nil {
			t.Fatal(err)
		}
	}
	h.fire(h.cron.Due(context.Background(), at.Add(3*time.Minute), cron.Tick))
	if pick := <-picks; pick.Fired != "c1" {
		t.Errorf("the first fire started a turn marked %q, want c1, so the push guard never knows", pick.Fired)
	}
	h.mu.Lock()
	unread := slices.Clone(h.unread)
	h.mu.Unlock()
	if len(unread) != 1 || !strings.Contains(unread[0].Prompt, "cron c2 fired") || !h.inbox.Holds(unread[0].Prompt) {
		t.Errorf("the second fire is not waiting in the lead's inbox as c2's prompt: %+v", unread)
	}
	if len(h.steering.ready) > 0 {
		t.Errorf("the second fire was steered, so the turn records it as the person's words")
	}
	release <- struct{}{}
	if pick := <-picks; pick.Fired != "c2" {
		t.Errorf("the unread fire started a turn marked %q, want c2", pick.Fired)
	}
	if job, _ := h.cron.Job("c1"); job.LastResult != "the build passes" {
		t.Errorf("c1 ended its turn with last result %q, want the answer, and it stays waiting", job.LastResult)
	}
	if job, _ := h.cron.Job("c2"); job.LastResult != "" {
		t.Errorf("c2's prompt was never read and the turn's answer was booked to it: %q", job.LastResult)
	}
}
