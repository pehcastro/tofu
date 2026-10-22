package turn

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func TestTheLeadListsARunningAndAFinishedSubAgentWithoutWaitingForTheRunningOne(t *testing.T) {
	const ordersRoute = "add the orders route"
	users, err := json.Marshal(spawnArgs{Task: usersRoute, Owns: []string{"src/users.ts"}, Agent: "ts-dev", Effort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	orders, err := json.Marshal(spawnArgs{Task: ordersRoute, Owns: []string{"src/orders.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	model := newCrew(map[string][]llm.Decision{
		leadKey: {
			toolCallDecision(llm.ToolCall{ID: "call-users", Name: "spawn", Arguments: users}, llm.ToolCall{ID: "call-orders", Name: "spawn", Arguments: orders}),
			toolCallDecision(llm.ToolCall{ID: "call-list", Name: "subagents", Arguments: json.RawMessage(`{}`)}),
			claimDecision("ts-dev-1 is running"), claimDecision("noted"), claimDecision("done"), claimDecision("done"),
		},
		usersRoute: {
			toolCallDecision(llm.ToolCall{ID: "call-read", Name: "read", Arguments: json.RawMessage(`{"path":"plan.md"}`)}),
			claimDecision("the users route is added"),
		},
		ordersRoute: {claimDecision("the orders route is added")},
	})
	midRequest, listing := model.hold(usersRoute, 2), model.hold(leadKey, 2)
	defer midRequest()
	defer listing()
	lead := crewLead(t, model)
	spawn := lead.Tools.byName["spawn"].(*SpawnTool)
	spawn.SubAgents.Open = func(_ subagent.Definition, effort llm.Effort) (SubAgentModel, error) {
		return SubAgentModel{Effort: effort}, nil
	}
	var aMinuteLater atomic.Bool
	spawned := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	spawn.base.Now = func() time.Time {
		if aMinuteLater.Load() {
			return spawned.Add(time.Minute)
		}
		return spawned
	}
	led := startLead(context.Background(), lead, nil)
	finished := func() bool {
		for _, agent := range spawn.roster.SubAgents() {
			if agent.ID == "sub-1" {
				return agent.State == subagent.Finished
			}
		}
		return false
	}
	waitFor(t, "sub-1 to finish and ts-dev-1 to be mid its second model request", func() bool {
		return finished() && len(model.requests(usersRoute)) == 2
	})
	aMinuteLater.Store(true)
	listing()
	waitFor(t, "the lead's request after its subagents call", func() bool { return len(model.requests(leadKey)) >= 3 })
	listed := answerTo(t, model, "call-list")
	if model.answered(usersRoute) != 1 {
		t.Fatalf("ts-dev-1 answered %d times by the time the list returned, want 1: the list waited for it", model.answered(usersRoute))
	}
	t.Logf("the list:\n%s", listed)
	var running, ended string
	for _, line := range strings.Split(listed, "\n") {
		switch {
		case strings.HasPrefix(line, "ts-dev-1:"):
			running = line
		case strings.HasPrefix(line, "sub-1:"):
			ended = line
		}
	}
	for _, want := range []string{"ts-dev", "running, has run 1m0s", "effort low", "last tool read", "1 steps, 1 tool calls"} {
		if !strings.Contains(running, want) {
			t.Errorf("ts-dev-1's line %q does not say %q", running, want)
		}
	}
	if !strings.Contains(ended, "finished, ran 0s") || strings.Contains(ended, "running") {
		t.Errorf("sub-1's line %q does not say it finished at its last step, a minute before the list", ended)
	}
	midRequest()
	led.wait(t)
}
