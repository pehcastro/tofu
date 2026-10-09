package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"tofu/internal/turn"
)

func TestTwoAsksWaitingAtOnceEachGetTheAnswerSentForThem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{})
	s := &server{ServeConfig: ServeConfig{Host: h}, box: newOutbox(), pending: map[string]ApprovalRequest{},
		items: items{tools: map[string]openTool{}, agents: map[string]SubAgentRow{}}}
	awaited := make(chan string, 2)
	emit := func(event Event) {
		s.publish(event)
		if event.Kind == EventAwaitPerson {
			awaited <- event.ID
		}
	}
	person := awaitPerson(emit, h.asks)
	for round := range 50 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		got := map[string]chan turn.PersonAnswer{}
		var order []string
		for _, name := range []string{"a", "b"} {
			id := fmt.Sprintf("ask-%s-%d", name, round)
			got[id] = make(chan turn.PersonAnswer, 1)
			go func() {
				answer, _ := person(ctx, turn.GateRequest{Tool: "bash", Args: json.RawMessage(`{"command":"echo ` + id + `"}`)}, turn.GateDecision{ID: id})
				got[id] <- answer
			}()
			order = append(order, <-awaited)
		}
		first, second := order[0], order[1]
		if err := errors.Join(s.answered(json.RawMessage(`"`+second+`"`), json.RawMessage(`{"decision":"reject_once"}`)),
			s.answered(json.RawMessage(`"`+first+`"`), json.RawMessage(`{"decision":"allow_once"}`))); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		for id, want := range map[string]turn.PersonAnswer{first: turn.PersonAllowedOnce, second: turn.PersonDenied} {
			select {
			case answer := <-got[id]:
				if answer != want {
					cancel()
					t.Fatalf("round %d: %s was answered %v, the answer sent for the other ask; want %v", round, id, answer, want)
				}
			case <-ctx.Done():
				t.Fatalf("round %d: %s was never answered, its answer was lost", round, id)
			}
		}
		cancel()
	}
}
