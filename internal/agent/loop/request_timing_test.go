package loop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/platform/outside"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
)

type slowModel struct {
	flight     time.Duration
	firstToken int64
}

func (m slowModel) Ask(ctx context.Context, _ core.Request) (core.Decision, error) {
	defer outside.Wait(ctx, outside.Model)()
	select {
	case <-ctx.Done():
		return core.Decision{}, ctx.Err()
	case <-time.After(m.flight):
	}
	return core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: "done", FirstTokenMS: m.firstToken}, nil
}

func TestATurnWritesItsRequestFirstTokenAndTurnWallsWithTheModelTimeOutside(t *testing.T) {
	model := slowModel{flight: 80 * time.Millisecond, firstToken: 37}
	store := session.NewStore(t.TempDir())
	config := baseConfig(t, model, NewRegistry())
	config.Sessions = store
	config.EndedSession = nil

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	events, err := store.Events(row.Session)
	if err != nil {
		t.Fatalf("read the events back: %v", err)
	}
	requests := map[string]bool{}
	for _, event := range events {
		if event.Kind != session.EventRequest && event.Kind != session.EventTurnEnd {
			continue
		}
		requests[event.Request] = event.Kind == session.EventRequest
		var body map[string]json.RawMessage
		if err := json.Unmarshal(event.Body, &body); err != nil {
			t.Fatalf("a %s event does not parse: %v", event.Kind, err)
		}
		for _, old := range []string{"duration_ms", "first_token_ms", "last_byte_ms", "wall_clock_ms"} {
			if _, kept := body[old]; kept {
				t.Errorf("the %s event still carries %s: %s", event.Kind, old, event.Body)
			}
		}
	}
	found := map[walls.Op]session.WallEvent{}
	for _, event := range events {
		if wall, err := session.DecodeWall(event); err == nil {
			if wall.OutsideMS != nil {
				t.Logf("wall %s %s: %d ms, outside %d, request %q", wall.Op, wall.Name, wall.MS, *wall.OutsideMS, wall.Request)
			}
			found[walls.Op(wall.Op)] = wall
		}
	}
	request, turn, first := found[walls.OpRequest], found[walls.OpTurn], found[walls.OpFirstToken]
	switch {
	case request.OutsideMS == nil || !requests[request.Request]:
		t.Fatalf("no request wall with outside_ms tied to a request event: %+v", request)
	case request.MS < model.flight.Milliseconds() || *request.OutsideMS < model.flight.Milliseconds() || *request.OutsideMS > request.MS:
		t.Errorf("the request wall is %d ms with %d outside, want the %s flight inside both", request.MS, *request.OutsideMS, model.flight)
	case first.OutsideMS == nil || first.MS != model.firstToken || *first.OutsideMS != first.MS || first.Request != request.Request:
		t.Errorf("the first token wall is %+v, want %d ms, all of it outside, on request %s", first, model.firstToken, request.Request)
	case turn.OutsideMS == nil || turn.Name != "lead" || *turn.OutsideMS < model.flight.Milliseconds() || turn.MS < *turn.OutsideMS:
		t.Errorf("the turn wall is %+v, want the lead's turn holding the flight as outside time", turn)
	}
}
