package turn

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func messageTo(id, to, text string) llm.Decision {
	args, err := json.Marshal(messageArgs{To: to, Text: text})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "message", Arguments: args})
}

func answerTo(t *testing.T, model *crew, call string) string {
	t.Helper()
	for _, request := range model.requests(leadKey) {
		for _, message := range request.Messages {
			if message.Role == llm.RoleTool && message.ToolCallID == call {
				return message.Content
			}
		}
	}
	t.Fatalf("the lead was never asked after its %s call", call)
	return ""
}

func TestAMessageToASubAgentSpawnedInAnEarlierTurnReachesIt(t *testing.T) {
	model := newCrew(map[string][]llm.Decision{
		leadKey: {
			spawnCall("call-spawn", usersRoute, "src/users.ts"), claimDecision("sub-1 is on it"),
			messageTo("call-running", "sub-1", "use port 3003"), claimDecision("told sub-1"),
			messageTo("call-ended", "sub-1", "also add /health"), claimDecision("asked sub-1 for more"),
			claimDecision("all done"),
		},
		usersRoute: {
			toolCallDecision(llm.ToolCall{ID: "call-read", Name: "read", Arguments: json.RawMessage(`{"path":"plan.md"}`)}),
			claimDecision("the users route is added on 3003"),
			claimDecision("the health route is added"),
		},
	})
	release, again := model.hold(usersRoute, 1), model.hold(usersRoute, 3)
	defer release()
	defer again()
	lead := crewLead(t, model)
	typed := make(chan string, 1)
	led := startLead(context.Background(), lead, typed)
	waitFor(t, "the lead's first turn to end with sub-1 running", func() bool { return len(led.turns()) == 1 })
	typed <- "tell sub-1 to use port 3003"
	waitFor(t, "the lead's second turn to end", func() bool { return len(led.turns()) == 2 })
	release()
	waitFor(t, "the lead's third turn, started by sub-1's report, to end", func() bool { return len(led.turns()) == 3 })
	again()
	turns := led.wait(t)

	if said := answerTo(t, model, "call-running"); !strings.Contains(said, "sub-1 is running and reads this") {
		t.Errorf("a message to sub-1 while it ran from the turn before answered %q, want it accepted", said)
	}
	asked := model.requests(usersRoute)
	if len(asked) != 3 || lastUser(asked[1]) != "use port 3003" {
		t.Fatalf("sub-1 was asked %d times, and its second step's newest user message is not the lead's message", len(asked))
	}
	if said := answerTo(t, model, "call-ended"); !strings.Contains(said, "sub-1 resumes in the background") {
		t.Errorf("a message to sub-1 after it ended answered %q, want it resumed", said)
	}
	resumed := asked[2].Messages
	if !slices.ContainsFunc(resumed, func(message llm.Message) bool {
		return message.Role == llm.RoleAssistant && strings.Contains(message.Content, "the users route is added on 3003")
	}) || !strings.HasSuffix(lastUser(asked[2]), "also add /health") {
		t.Errorf("the resumed sub-1 did not see its own history and the lead's text: %+v", resumed)
	}
	if last := turns[len(turns)-1]; len(turns) != 4 || !strings.Contains(last.Task, "the health route is added") {
		t.Errorf("the lead ran %d turns and the last began %q, want 4 ending on sub-1's second report", len(turns), last.Task)
	}
}

func TestTheLeadStopsOneOfTwoRunningSubAgentsAndTheOtherKeepsRunning(t *testing.T) {
	const ordersRoute = "add the orders route"
	stop, err := json.Marshal(messageArgs{To: "sub-1", Stop: true})
	if err != nil {
		t.Fatal(err)
	}
	model := newCrew(map[string][]llm.Decision{
		leadKey: {spawnCall("call-users", usersRoute, "src/users.ts"), spawnCall("call-orders", ordersRoute, "src/orders.ts"),
			toolCallDecision(llm.ToolCall{ID: "call-stop", Name: "message", Arguments: stop}), claimDecision("sub-1 is stopped"), claimDecision("noted"), claimDecision("done")},
		usersRoute:  {claimDecision("the users route is added")},
		ordersRoute: {claimDecision("the orders route is added")},
	})
	users, orders := model.hold(usersRoute, 1), model.hold(ordersRoute, 1)
	defer users()
	defer orders()
	lead := crewLead(t, model)
	spawn := lead.Tools.byName["spawn"].(*SpawnTool)
	led := startLead(context.Background(), lead, nil)
	states := func() map[string]subagent.State {
		held := map[string]subagent.State{}
		for _, agent := range spawn.roster.SubAgents() {
			held[agent.ID] = agent.State
		}
		return held
	}
	waitFor(t, "sub-1 to stop", func() bool { return states()["sub-1"] == subagent.Parked })
	if said := answerTo(t, model, "call-stop"); strings.HasPrefix(said, "error:") || states()["sub-2"] != subagent.Working || model.answered(ordersRoute) != 0 {
		t.Fatalf("stopping sub-1 answered %q and left sub-2 %s, want sub-1 stopped and sub-2 still running", said, states()["sub-2"])
	}
	orders()
	led.wait(t)
	if states()["sub-2"] != subagent.Finished || model.answered(ordersRoute) != 1 {
		t.Fatalf("sub-2 ended %s, want it to finish its work after sub-1 was stopped", states()["sub-2"])
	}
}

func TestAMessageToAnUnknownNameIsRefusedListingTheNames(t *testing.T) {
	brief, err := json.Marshal(spawnArgs{Task: usersRoute, Owns: []string{"src/users.ts"}, Agent: "ts-dev"})
	if err != nil {
		t.Fatal(err)
	}
	model := newCrew(map[string][]llm.Decision{
		leadKey: {toolCallDecision(llm.ToolCall{ID: "call-spawn", Name: "spawn", Arguments: brief}), messageTo("call-message", "ts-dev-9", "also add /health"),
			claimDecision("ts-dev-9 is no one"), claimDecision("done")},
		usersRoute: {claimDecision("the users route is added")},
	})
	lead := crewLead(t, model)
	startLead(context.Background(), lead, nil).wait(t)
	if refused := answerTo(t, model, "call-message"); !strings.HasPrefix(refused, "error:") || !strings.Contains(refused, "ts-dev-9") || !strings.Contains(refused, "ts-dev-1") {
		t.Errorf("a message to ts-dev-9 answered %q, want a refusal naming ts-dev-1", refused)
	}
}
