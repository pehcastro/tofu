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

func TestASpawnRunsItsSubAgentAtTheEffortItNames(t *testing.T) {
	for _, spawned := range []struct {
		effort    string
		lead      []llm.Decision
		subAgent  []llm.Decision
		wantAsked []llm.Effort
		wantSaid  string
	}{
		{"low", []llm.Decision{messageTo("call-message", "ts-dev-1", "once more"), claimDecision("asked again"), claimDecision("done")},
			[]llm.Decision{claimDecision("done"), claimDecision("again")}, []llm.Effort{llm.EffortLow, llm.EffortLow}, "at effort low"},
		{"", []llm.Decision{claimDecision("done")}, []llm.Decision{claimDecision("done")}, []llm.Effort{""}, "at effort medium"},
		{"minimal", []llm.Decision{messageDecision()}, nil, []llm.Effort{llm.EffortMinimal}, "it takes low, medium, high"},
		{"turbo", []llm.Decision{messageDecision()}, nil, nil, "is no thinking effort"},
	} {
		t.Run(cmp.Or(spawned.effort, "inherited"), func(t *testing.T) {
			orchestrator, spawn := orchestratorTurn(t, t.TempDir(), nil)
			opener := &effortOpener{lead: llm.EffortMedium, takes: []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh}}
			spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel}}
			spawn.SubAgents.Open = opener.open
			args, _ := json.Marshal(map[string]any{"task": usersRoute, "owns": []string{"src/users.ts"}, "agent": "ts-dev", "effort": spawned.effort})
			model := newCrew(map[string][]llm.Decision{
				leadKey:    append([]llm.Decision{toolCallDecision(llm.ToolCall{ID: "call-spawn", Name: "spawn", Arguments: args})}, spawned.lead...),
				usersRoute: spawned.subAgent,
			})
			release := model.hold(usersRoute, 1)
			defer release()
			orchestrator.Model, spawn.base.Model = model, model
			led := startLead(context.Background(), orchestrator, nil)
			waitFor(t, "the lead's first turn to end", func() bool { return len(led.turns()) == 1 })
			release()
			led.wait(t)
			said := answerTo(t, model, "call-spawn")
			t.Logf("opened at %q, the spawn said:\n%.300s", opener.asked, said)
			if !slices.Equal(opener.asked, spawned.wantAsked) || !strings.Contains(said, spawned.wantSaid) {
				t.Fatalf("effort %q: the opener was asked %q, want %q, and the spawn should say %q", spawned.effort, opener.asked, spawned.wantAsked, spawned.wantSaid)
			}
		})
	}
}

func TestTheLeadIsToldToSpawnAtLowAndSettleTheBrief(t *testing.T) {
	const wantRule = "start each sub-agent at low effort, and settle every open choice in the brief yourself: the exact behaviour, the edge cases the spec leaves open, which files change. raise the effort, low to medium and medium to high, when a sub-agent at its level fails the same check twice, returns work that does not do what the brief asked or that you would not accept, or breaks the rules it was given, such as writing outside the files it owns or skipping the checks the brief named. raise it by spawning the piece again at the higher level, with what went wrong named in the brief, rather than sending more messages at the same level. the next piece starts at low again."
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
