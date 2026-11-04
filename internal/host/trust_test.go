package host

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"tofu/internal/agent/loop"
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
	}, h.asks, &loop.Config{})
	hooks := func(command string) loop.GateRequest {
		args, _ := json.Marshal(map[string]string{"command": command})
		return loop.GateRequest{Tool: "hooks", Args: args}
	}
	ask := func(request loop.GateRequest, answer Answer) (loop.PersonAnswer, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		got := make(chan loop.PersonAnswer, 1)
		go func() {
			answered, _ := person(ctx, request, loop.GateDecision{})
			got <- answered
		}()
		select {
		case id := <-asked:
			h.AnswerAsk(id, answer, nil)
			return <-got, true
		case answered := <-got:
			return answered, false
		}
	}
	if answered, wasAsked := ask(hooks("GateVerdict *: bash .tofu/hooks/gate.sh (project, .tofu/hooks.json)"), AlwaysHere); !wasAsked || answered != loop.PersonAlwaysHere {
		t.Fatalf("the first trust ask was not put to the person, or not answered always: asked %v, %v", wasAsked, answered)
	}
	changed := hooks("GateVerdict *: bash .tofu/hooks/gate.sh (project, .tofu/hooks.json)")
	if answered, wasAsked := ask(changed, Denied); !wasAsked || answered != loop.PersonDenied {
		t.Fatalf("hooks that changed after always here were trusted without asking: asked %v, answer %v", wasAsked, answered)
	}
}
