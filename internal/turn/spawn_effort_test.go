package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
)

type effortOpener struct {
	asked []llm.Effort
	lead  llm.Effort
	takes []llm.Effort
}

func (o *effortOpener) open(_ subagent.Definition, wanted llm.Effort) (SubAgentModel, error) {
	o.asked = append(o.asked, wanted)
	if wanted != "" && !slices.Contains(o.takes, wanted) {
		return SubAgentModel{}, fmt.Errorf("effort %s for stub/model: it takes %s, so the level would be changed without a word", wanted, llm.EffortList(o.takes))
	}
	return SubAgentModel{Slug: "stub/model", Effort: cmp.Or(wanted, o.lead)}, nil
}

func spawnResult(model *stubModel, call string) string {
	for _, request := range model.requests {
		for _, message := range request.Messages {
			if message.Role == llm.RoleTool && message.ToolCallID == call {
				return message.Content
			}
		}
	}
	return ""
}

func TestASpawnRunsItsSubAgentAtTheEffortItNames(t *testing.T) {
	for _, spawned := range []struct {
		effort    string
		then      []llm.Decision
		wantAsked []llm.Effort
		wantSaid  string
	}{
		{"low", []llm.Decision{claimDecision("done"), toolCallDecision(llm.ToolCall{ID: "call-message", Name: "message", Arguments: json.RawMessage(`{"to":"ts-dev-1","text":"once more"}`)}), claimDecision("again"), messageDecision()},
			[]llm.Effort{llm.EffortLow, llm.EffortLow}, "at effort low"},
		{"", []llm.Decision{claimDecision("done"), messageDecision()}, []llm.Effort{""}, "at effort medium"},
		{"minimal", []llm.Decision{messageDecision()}, []llm.Effort{llm.EffortMinimal}, "it takes low, medium, high"},
		{"turbo", []llm.Decision{messageDecision()}, nil, "is no thinking effort"},
	} {
		t.Run(cmp.Or(spawned.effort, "inherited"), func(t *testing.T) {
			orchestrator, spawn := orchestratorTurn(t, t.TempDir(), nil)
			opener := &effortOpener{lead: llm.EffortMedium, takes: []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh}}
			spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel}}
			spawn.SubAgents.Open = opener.open
			brief, _ := json.Marshal(map[string]any{"task": "add the users route", "owns": []string{"src/users.ts"}, "agent": "ts-dev", "effort": spawned.effort})
			model := &stubModel{decisions: append([]llm.Decision{toolCallDecision(llm.ToolCall{ID: "call-spawn", Name: "spawn", Arguments: brief})}, spawned.then...)}
			orchestrator.Model, spawn.base.Model = model, model
			if _, err := Run(context.Background(), orchestrator); err != nil {
				t.Fatal(err)
			}
			said := spawnResult(model, "call-spawn")
			t.Logf("opened at %q, the spawn said:\n%.300s", opener.asked, said)
			if !slices.Equal(opener.asked, spawned.wantAsked) || !strings.Contains(said, spawned.wantSaid) {
				t.Fatalf("effort %q: the opener was asked %q, want %q, and the spawn should say %q", spawned.effort, opener.asked, spawned.wantAsked, spawned.wantSaid)
			}
		})
	}
}

func TestTheLeadIsToldToSpawnAtLowAndSettleTheBrief(t *testing.T) {
	const wantRule = "choose each sub-agent's effort when you spawn it. low is the default and is enough for almost every piece: on large repositories low matched medium and high on every hidden check and was faster. before you spawn, settle every open choice in the brief yourself: the exact behaviour, the edge cases the spec leaves open, which files change. a brief that settles them is followed at low. choose medium only when the sub-agent must make design decisions you cannot settle from what you have read, and name those decisions in the brief. touching shared code, a schema or many files is not a reason for more effort; it is a reason for a more exact brief."
	const wantEffort = "how hard the sub-agent thinks. low unless it must make design decisions the brief does not settle. left out, it thinks as hard as you do"
	rules, err := rule.LoadDir("../../library")
	if err != nil {
		t.Fatal(err)
	}
	shipped := slices.IndexFunc(rules, func(r rule.Rule) bool { return r.ID == "spawn_effort" })
	if shipped < 0 || rules[shipped].Text != wantRule {
		t.Fatalf("spawn_effort at index %d does not read as the ticket says", shipped)
	}
	_, spawn := orchestratorTurn(t, t.TempDir(), nil)
	properties := spawn.Definition().Parameters.(map[string]any)["properties"].(map[string]any)
	if said := properties["effort"].(map[string]any)["description"]; said != wantEffort {
		t.Fatalf("the effort argument is described as %q", said)
	}
}
