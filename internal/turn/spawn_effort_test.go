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
	takes map[string][]llm.Effort
}

func (o *effortOpener) taken(definition subagent.Definition) ([]llm.Effort, error) {
	return o.takes[definition.Name], nil
}

func (o *effortOpener) open(definition subagent.Definition, wanted llm.Effort) (SubAgentModel, error) {
	o.asked = append(o.asked, wanted)
	takes := o.takes[definition.Name]
	switch {
	case wanted != "" && len(takes) == 0:
		return SubAgentModel{}, fmt.Errorf("effort %s for stub/model: it sends no reasoning effort, so the level would be dropped without a word", wanted)
	case wanted != "" && !slices.Contains(takes, wanted):
		return SubAgentModel{}, fmt.Errorf("effort %s for stub/model: it takes %s, so the level would be changed without a word", wanted, llm.EffortList(takes))
	}
	return SubAgentModel{Slug: "stub/model", Effort: cmp.Or(wanted, o.lead)}, nil
}

func TestASpawnRunsItsSubAgentAtTheNearestEffortItsModelTakes(t *testing.T) {
	lowToHigh := []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh}
	ran := []llm.Decision{claimDecision("done")}
	for _, spawned := range []struct {
		name         string
		effort       string
		takes        []llm.Effort
		lead         []llm.Decision
		subAgent     []llm.Decision
		wantAsked    []llm.Effort
		wantSaid     string
		leadRequests int
	}{
		{"exact then resumed", "low", lowToHigh, []llm.Decision{messageTo("call-message", "ts-dev-1", "once more"), claimDecision("asked again"), claimDecision("done")},
			[]llm.Decision{claimDecision("done"), claimDecision("again")}, []llm.Effort{llm.EffortLow, llm.EffortLow}, "at effort low", 4},
		{"inherited", "", lowToHigh, []llm.Decision{claimDecision("done")}, ran, []llm.Effort{""}, "at effort medium", 2},
		{"below the lowest", "minimal", lowToHigh, []llm.Decision{claimDecision("done")}, ran, []llm.Effort{llm.EffortLow},
			"effort minimal is not one stub/model takes, so it runs at low", 2},
		{"below the lowest, resumed", "minimal", lowToHigh, []llm.Decision{messageTo("call-message", "ts-dev-1", "once more"), claimDecision("asked again"), claimDecision("done")},
			[]llm.Decision{claimDecision("done"), claimDecision("again")}, []llm.Effort{llm.EffortLow, llm.EffortLow}, "so it runs at low", 4},
		{"above the highest", "max", lowToHigh, []llm.Decision{claimDecision("done")}, ran, []llm.Effort{llm.EffortHigh},
			"effort max is not one stub/model takes, so it runs at high", 2},
		{"between two, a tie goes up", "medium", []llm.Effort{llm.EffortLow, llm.EffortHigh}, []llm.Decision{claimDecision("done")}, ran, []llm.Effort{llm.EffortHigh},
			"effort medium is not one stub/model takes, so it runs at high", 2},
		{"between two, the nearer", "xhigh", []llm.Effort{llm.EffortLow, llm.EffortMax}, []llm.Decision{claimDecision("done")}, ran, []llm.Effort{llm.EffortMax},
			"effort xhigh is not one stub/model takes, so it runs at max", 2},
		{"a model that takes none", "low", nil, []llm.Decision{messageDecision()}, nil, []llm.Effort{llm.EffortLow}, "it sends no reasoning effort", 2},
		{"an unknown word", "turbo", lowToHigh, []llm.Decision{messageDecision()}, nil, nil, "is no thinking effort", 2},
	} {
		t.Run(spawned.name, func(t *testing.T) {
			orchestrator, spawn := orchestratorTurn(t, t.TempDir(), nil)
			opener := &effortOpener{lead: llm.EffortMedium, takes: map[string][]llm.Effort{"ts-dev": spawned.takes}}
			spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel}}
			spawn.SubAgents.Open, spawn.SubAgents.Takes = opener.open, opener.taken
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
			leadRequests := len(model.requests(leadKey))
			t.Logf("effort %q on a model taking [%s]: opened at %q, %d lead requests, the spawn said:\n%.300s", spawned.effort, llm.EffortList(spawned.takes), opener.asked, leadRequests, said)
			if !slices.Equal(opener.asked, spawned.wantAsked) || !strings.Contains(said, spawned.wantSaid) || leadRequests != spawned.leadRequests {
				t.Fatalf("effort %q: the opener was asked %q, want %q; the spawn should say %q; %d lead requests, want %d",
					spawned.effort, opener.asked, spawned.wantAsked, spawned.wantSaid, leadRequests, spawned.leadRequests)
			}
		})
	}
}

func TestTheLeadIsToldToSpawnAtLowAndSettleTheBrief(t *testing.T) {
	const wantRule = "start each sub-agent at low effort, and settle every open choice in the brief yourself: the exact behaviour, the edge cases the spec leaves open, which files change. raise the effort, low to medium and medium to high, when a sub-agent at its level fails the same check twice, returns work that does not do what the brief asked or that you would not accept, or breaks the rules it was given, such as writing outside the files it owns or skipping the checks the brief named. raise it by spawning the piece again at the higher level, with what went wrong named in the brief, rather than sending more messages at the same level. the next piece starts at low again."
	rules, err := rule.LoadDir("../../library")
	if err != nil {
		t.Fatal(err)
	}
	shipped := slices.IndexFunc(rules, func(r rule.Rule) bool { return r.ID == "spawn_effort" })
	if shipped < 0 || rules[shipped].Text != wantRule {
		t.Fatalf("spawn_effort at index %d does not read as the ticket says", shipped)
	}
	const told = "how hard the sub-agent thinks. low unless it must make design decisions the brief does not settle. left out, it thinks as hard as you do. "
	lowToHigh := []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh}
	for _, offered := range []struct {
		name     string
		takes    map[string][]llm.Effort
		wantEnum []string
		wantSaid string
	}{
		{"one model", map[string][]llm.Effort{"": lowToHigh, "ts-dev": lowToHigh}, []string{"low", "medium", "high"},
			told + "the model takes low, medium, high"},
		{"two models", map[string][]llm.Effort{"": {llm.EffortMinimal, llm.EffortLow}, "ts-dev": lowToHigh}, []string{"minimal", "low", "medium", "high"},
			told + "left out it takes minimal, low; ts-dev takes low, medium, high"},
		{"one takes none", map[string][]llm.Effort{"ts-dev": lowToHigh}, []string{"low", "medium", "high"},
			told + "left out it takes none, so leave effort out; ts-dev takes low, medium, high"},
		{"none takes any", map[string][]llm.Effort{}, nil, ""},
	} {
		t.Run(offered.name, func(t *testing.T) {
			_, spawn := orchestratorTurn(t, t.TempDir(), nil)
			opener := &effortOpener{takes: offered.takes}
			spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Runs: subagent.RunsModel}, {Name: "off", Runs: subagent.RunsDisabled}}
			spawn.SubAgents.Open, spawn.SubAgents.Takes = opener.open, opener.taken
			defined, _ := json.Marshal(spawn.Definition().Parameters.(map[string]any)["properties"].(map[string]any)["effort"])
			t.Logf("effort: %s", defined)
			var effort struct {
				Enum        []string `json:"enum"`
				Description string   `json:"description"`
			}
			_ = json.Unmarshal(defined, &effort)
			if !slices.Equal(effort.Enum, offered.wantEnum) || effort.Description != offered.wantSaid {
				t.Fatalf("effort offered as %s", defined)
			}
		})
	}
}
