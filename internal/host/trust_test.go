package host

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"tofu/internal/turn"
)

func TestAStandingAnswerNeverTrustsHooksThatChangedAfterIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{})
	asked := make(chan string, 4)
	person := awaitPerson(func(event Event) {
		if event.Kind == EventAwaitPerson {
			asked <- event.ID
		}
	}, h.asks)
	hooks := func(command string) turn.GateRequest {
		args, _ := json.Marshal(map[string]string{"command": command})
		return turn.GateRequest{Tool: "hooks", Args: args}
	}
	ask := func(request turn.GateRequest, answer Answer) (turn.PersonAnswer, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		got := make(chan turn.PersonAnswer, 1)
		go func() {
			answered, _ := person(ctx, request, turn.GateDecision{})
			got <- answered
		}()
		select {
		case id := <-asked:
			h.AnswerAsk(id, answer)
			return <-got, true
		case answered := <-got:
			return answered, false
		}
	}
	if answered, wasAsked := ask(hooks("GateVerdict *: bash .tofu/hooks/gate.sh (project, .tofu/hooks.json)"), AlwaysHere); !wasAsked || answered != turn.PersonAlwaysHere {
		t.Fatalf("the first trust ask was not put to the person, or not answered always: asked %v, %v", wasAsked, answered)
	}
	changed := hooks("GateVerdict *: bash .tofu/hooks/gate.sh (project, .tofu/hooks.json)")
	if answered, wasAsked := ask(changed, Denied); !wasAsked || answered != turn.PersonDenied {
		t.Fatalf("hooks that changed after always here were trusted without asking: asked %v, answer %v", wasAsked, answered)
	}
}
