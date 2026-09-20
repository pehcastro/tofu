package tui

import (
	"testing"
	"time"

	"boji/interface/tui/session"
)

const repaintStretch = 12

func paintsOver(app *App, at *time.Time, ticks int) (int, int) {
	drawn := make(map[string]bool, ticks)
	painted := 0
	for range ticks {
		if !app.ticking {
			break
		}
		*at = at.Add(session.TickInterval)
		app.Update(tickMsg(*at))
		drawn[app.View().Content] = true
		painted++
	}
	return painted, len(drawn)
}

func settle(app *App, at *time.Time) {
	*at = at.Add(session.TickInterval)
	app.Update(tickMsg(*at))
}

func TestATurnWaitingOnTheModelKeepsRepaintingAndItsClockAdvances(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	app.Update(Event{Kind: EventToolResult, ID: "c2", Text: "14 lines, 64 bytes"})
	settle(app, &at)
	before := turnElapsed(t, app)

	painted, different := paintsOver(app, &at, repaintStretch)
	if painted != repaintStretch {
		t.Errorf("a turn waiting on the model painted %d frames over %d ticks, want one per tick", painted, repaintStretch)
	}
	if different != repaintStretch {
		t.Errorf("a turn waiting on the model painted %d different frames out of %d, want every one to move", different, painted)
	}
	if after := turnElapsed(t, app); after == before {
		t.Errorf("three seconds of waiting on the model left the clock at %q", after)
	}
}

func TestWaitingOnThePersonRepaintsAFrameThatDoesNotMove(t *testing.T) {
	at := fixedStart()
	app := liveApp(t, &at)
	app.Update(Event{Kind: EventAwaitPerson})
	settle(app, &at)
	before := turnRow(t, app)
	painted, _ := paintsOver(app, &at, repaintStretch)
	if painted != repaintStretch {
		t.Errorf("a turn waiting on the person painted %d frames over %d ticks, want one per tick", painted, repaintStretch)
	}
	if after := turnRow(t, app); after != before {
		t.Errorf("waiting on the person moved the running row from %q to %q", before, after)
	}
}
