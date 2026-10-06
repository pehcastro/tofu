package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"tofu/internal/cron"
)

func TestAFiredJobStartsATurnWhenIdleAndSteersWhenBusy(t *testing.T) {
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
	select {
	case steered := <-h.steering:
		if !strings.Contains(steered, "cron c2 fired") {
			t.Errorf("the second fire steered %q, want c2's prompt", steered)
		}
	case pick := <-picks:
		t.Errorf("the second fire started a second turn, marked %q", pick.Fired)
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
