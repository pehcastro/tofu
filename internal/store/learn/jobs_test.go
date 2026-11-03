package learn

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"

	"tofu/internal/session"
)

type act struct {
	minute int
	agent  string
	origin string
	text   string
	tools  []string
	tokens int
	cost   float64
	legacy bool
}

func work(minute int, cost float64, tools ...string) act {
	return act{minute: minute, tools: tools, tokens: 100, cost: cost}
}

func asks(minute int, origin, text string) act {
	return act{minute: minute, origin: origin, text: text}
}

func writeWork(t *testing.T, store *session.Store, id string, acts ...act) {
	t.Helper()
	name := id
	log, err := store.Open(session.Header{ID: id, Name: &name, At: start()})
	if err != nil {
		t.Fatal(err)
	}
	add := func(event session.Event, body any) {
		if _, err := log.Append(event, body); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range acts {
		at := start().Add(time.Duration(a.minute) * time.Minute)
		if a.origin != "" {
			add(session.Event{Kind: session.EventMessage, At: at}, session.MessageBody{Role: session.RoleUser, Content: a.text, Origin: a.origin, TakenAt: &at})
			continue
		}
		step := session.StepBody{PromptTokens: a.tokens, CostUSD: a.cost}
		kind := session.EventRequest
		if a.legacy {
			kind = session.EventStep
			for _, tool := range a.tools {
				step.ToolCalls = append(step.ToolCalls, session.StepToolCall{Tool: tool})
			}
		}
		add(session.Event{Kind: kind, At: at, Agent: a.agent}, step)
		if a.legacy {
			continue
		}
		for _, tool := range a.tools {
			add(session.Event{Kind: session.EventToolCall, At: at.Add(time.Second), Agent: a.agent}, session.CallBody{Tool: tool, Args: json.RawMessage(`{}`)})
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestARepeatedJobIsTheSameToolsForTheSameRequestPricedFromEveryStepInItsWindow(t *testing.T) {
	nightly := "Write the nightly report: list the failed builds and open issues, then save the report."
	store := session.NewStore(t.TempDir())
	writeWork(t, store, "sample-a",
		asks(1, task, wrapper+nightly),
		work(2, 0.01, "bash", "bash", "read"),
		act{minute: 3, agent: "helper-1", tools: []string{"edit"}, tokens: 50, cost: 0.005},
		asks(10, typed, wrapper+"fix the typo in the readme heading"),
		work(11, 0.5, "read", "edit"),
		asks(20, typed, "why did you stop? keep working"),
	)
	writeWork(t, store, "sample-b",
		asks(1, task, wrapper+nightly),
		work(2, 0.02, "bash"),
		asks(3, steer, "also note the date at the top"),
		work(4, 0.03, "read"),
		asks(10, typed, "rename the config loader to settings loader"),
		work(11, 0.7, "read", "edit"),
		asks(20, typed, "what does the nightly report script do"),
		work(21, 0.9, "read"),
		asks(30, typed, "why did you stop? keep working"),
	)
	writeWork(t, store, "sample-c",
		asks(1, task, nightly),
		act{minute: 2, tools: []string{"bash", "read"}, tokens: 100, cost: 0.04, legacy: true},
	)
	headers, err := Recent(store, 10)
	if err != nil {
		t.Fatal(err)
	}
	run, err := Scan([]Source{{Store: store, Headers: headers}}, Known{}, start())
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Jobs) != 1 {
		t.Fatalf("want one repeated job, the nightly report; got %d: %+v", len(run.Jobs), run.Jobs)
	}
	job := run.Jobs[0]
	if job.Name != nightly || job.Times != 3 || !slices.Equal(job.Tools, []string{"bash", "read"}) || !slices.Equal(job.Sessions, []string{"sample-a", "sample-b", "sample-c"}) {
		t.Errorf("job %q, %d times, tools %v, sessions %v", job.Name, job.Times, job.Tools, job.Sessions)
	}
	want := 0.01 + 0.005 + 0.02 + 0.03 + 0.04
	if math.Abs(job.Cost-want) > 1e-9 || math.Abs(job.CostEach-want/3) > 1e-9 || job.Tokens != 450 {
		t.Errorf("cost %.4f each %.4f tokens %d, want %.4f, %.4f and 450", job.Cost, job.CostEach, job.Tokens, want, want/3)
	}
	if costs := []float64{job.Runs[0].Cost, job.Runs[1].Cost, job.Runs[2].Cost}; math.Abs(costs[0]-0.015) > 1e-9 || math.Abs(costs[1]-0.05) > 1e-9 || math.Abs(costs[2]-0.04) > 1e-9 {
		t.Errorf("each run's cost %v, want 0.015, 0.05 and 0.04", costs)
	}
}
