package turn

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
)

type slowModel struct {
	flight     time.Duration
	firstToken int64
}

func (m slowModel) Ask(ctx context.Context, _ llm.Request) (llm.Decision, error) {
	select {
	case <-ctx.Done():
		return llm.Decision{}, ctx.Err()
	case <-time.After(m.flight):
	}
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done", FirstTokenMS: m.firstToken}, nil
}

func TestARecordedRequestEventCarriesItsDurationAndItsFirstToken(t *testing.T) {
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
	requests := 0
	for _, event := range events {
		if event.Kind != session.EventRequest {
			continue
		}
		requests++
		var timing map[string]json.RawMessage
		if err := json.Unmarshal(event.Body, &timing); err != nil {
			t.Fatalf("a request event does not parse: %v", err)
		}
		var duration, firstToken int64
		for name, into := range map[string]*int64{"duration_ms": &duration, "first_token_ms": &firstToken} {
			raw, present := timing[name]
			if !present {
				t.Fatalf("the request event has no %s: %s", name, event.Body)
			}
			if err := json.Unmarshal(raw, into); err != nil {
				t.Fatalf("%s is %s, not a number of milliseconds", name, raw)
			}
		}
		if duration < model.flight.Milliseconds() {
			t.Fatalf("duration_ms %d is shorter than the %s the model took", duration, model.flight)
		}
		if firstToken != model.firstToken {
			t.Fatalf("first_token_ms %d is not the %d the decision carried", firstToken, model.firstToken)
		}
		t.Logf("request event: duration_ms %d, first_token_ms %d", duration, firstToken)
	}
	if requests == 0 {
		t.Fatal("the turn recorded no request event")
	}
}
