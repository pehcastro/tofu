package turn

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
)

type slowGate struct{ took time.Duration }

func (g slowGate) Decide(context.Context, GateRequest) (GateDecision, error) {
	time.Sleep(g.took)
	return GateDecision{ID: "shadow-row", Verdict: ledger.VerdictAllow}, nil
}

type timedModel struct {
	stubModel
	asked []time.Time
}

func (m *timedModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.asked = append(m.asked, time.Now())
	return m.stubModel.Ask(ctx, request)
}

func besideARead(spawning llm.Decision) llm.Decision {
	spawning.ToolCalls = append(slices.Clone(spawning.ToolCalls), llm.ToolCall{ID: "call-read", Name: "read", Arguments: json.RawMessage(`{"path":"notes.txt"}`)})
	return spawning
}

func TestALeadStepOfOnlyStartedSpawnsEndsTheTurnWithoutAnAnnounceRequest(t *testing.T) {
	announcing := spawnCall("call-spawn", usersRoute, "src/users.ts")
	announcing.Content = "spawning a sub-agent to add the users route"
	for _, c := range []struct {
		name, spawnedFrom, endsOn string
		first                     llm.Decision
		subAgentRuns              bool
		leadRequests              int
	}{
		{name: "text in the spawn reply ends on that text", first: announcing, subAgentRuns: true, leadRequests: 1, endsOn: announcing.Content},
		{name: "no text ends on the line tofu writes", first: spawnCall("call-spawn", usersRoute, "src/users.ts"), subAgentRuns: true, leadRequests: 1, endsOn: "sub-1 running: " + usersRoute},
		{name: "a read beside the spawn asks again", first: besideARead(spawnCall("call-spawn", usersRoute, "src/users.ts")), subAgentRuns: true, leadRequests: 2, endsOn: "sub-1 is on it"},
		{name: "a spawn that fails to start asks again", first: spawnCall("call-spawn", usersRoute), leadRequests: 2, endsOn: "sub-1 is on it"},
		{name: "a sub-agent's own spawn asks again", spawnedFrom: "turn-parent", first: announcing, subAgentRuns: true, leadRequests: 2, endsOn: "sub-1 is on it"},
	} {
		t.Run(c.name, func(t *testing.T) {
			model := newCrew(map[string][]llm.Decision{
				leadKey:    {c.first, claimDecision("sub-1 is on it")},
				usersRoute: {claimDecision("the users route is added")},
			})
			lead := crewLead(t, model)
			lead.SpawnedFrom = c.spawnedFrom
			row, err := Run(context.Background(), lead)
			if err != nil {
				t.Fatal(err)
			}
			if c.subAgentRuns {
				waitFor(t, "the sub-agent's answer", func() bool { return model.answered(usersRoute) == 1 })
			}
			last := row.Conversation[len(row.Conversation)-1]
			if got := len(model.requests(leadKey)); got != c.leadRequests || last.Role != llm.RoleAssistant || last.Content != c.endsOn {
				t.Errorf("the lead was asked %d times and ended on %s %q, want %d and %q", got, last.Role, last.Content, c.leadRequests, c.endsOn)
			}
		})
	}
}

func TestSpawnsInOneReplyAreNamedInTheOrderOfTheirCalls(t *testing.T) {
	routes := []string{"add the users route", "add the orders route", "add the items route", "add the carts route"}
	var calls []llm.ToolCall
	scripts := map[string][]llm.Decision{leadKey: nil}
	for i, route := range routes {
		calls = append(calls, spawnCall("call-"+strconv.Itoa(i+1), route, "src/"+strconv.Itoa(i+1)+".ts").ToolCalls[0])
		scripts[route] = []llm.Decision{claimDecision("done")}
	}
	scripts[leadKey] = []llm.Decision{toolCallDecision(calls...)}
	model := newCrew(scripts)
	lead := crewLead(t, model)
	if _, err := Run(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		waitFor(t, route, func() bool { return model.answered(route) == 1 })
	}
	for _, spawned := range lead.Tools.byName["spawn"].(*SpawnTool).Spawned() {
		if want := "sub-" + strings.TrimPrefix(spawned.Call, "call-"); spawned.ID != want {
			t.Errorf("%s was named %s, want %s, the order of its call in the reply", spawned.Call, spawned.ID, want)
		}
	}
}

func TestAShadowGateThatTakes300MillisecondsAddsNoTimeToAStep(t *testing.T) {
	model := &timedModel{stubModel: stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "noop", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}}
	row, err := Run(context.Background(), Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(namedTool("noop")), Task: "run noop",
		Gate: slowGate{took: 300 * time.Millisecond}, GateMode: GateShadow, ResultBytesCap: 4096, ArtifactDir: t.TempDir(), NoLastWord: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.asked) != 2 {
		t.Fatalf("the model was asked %d times, want 2", len(model.asked))
	}
	if step := model.asked[1].Sub(model.asked[0]); step >= 150*time.Millisecond {
		t.Errorf("the step between two asks took %s with a 300 ms shadow gate, want it not to wait on the gate", step)
	}
	if !slices.Contains(row.DecisionIDs, "shadow-row") {
		t.Errorf("the turn row does not carry the shadow decision: %v", row.DecisionIDs)
	}
}
