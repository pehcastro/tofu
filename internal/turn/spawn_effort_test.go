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
