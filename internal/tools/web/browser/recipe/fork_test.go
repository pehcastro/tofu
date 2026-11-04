package recipe

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"testing"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

type searchPage struct{ acts *int }

func (searchPage) Name() string { return "browser_act" }

func (searchPage) Definition() core.Tool {
	return core.Tool{Name: "browser_act", Parameters: map[string]any{"type": "object"}}
}

func (s searchPage) Run(context.Context, json.RawMessage) (contract.Result, error) {
	*s.acts++
	return contract.Result{Content: "1. click e7 \"Apply\": the page changed\n\n<<<ab12 begins>>>\ntab 1 https://www.airbnb.com.br/s/Atibaia--SP/homes?min_bedrooms=" + strconv.Itoa(*s.acts) + "\n- main\n<<<ab12 ends>>>"}, nil
}

type actsPastTheFactSheet struct{ asked int }

func (m *actsPastTheFactSheet) Ask(ctx context.Context, _ core.Request) (core.Decision, error) {
	if loop.AskedQuietly(ctx) {
		return core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: "goal: a house in Atibaia"}, nil
	}
	m.asked++
	if m.asked > konst.FactSheetLines+2 {
		return core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: "done"}, nil
	}
	args := json.RawMessage(`{"tab":1,"actions":[{"action":"click","ref":"e7"}]}`)
	return core.Decision{Build: "m1", Outcome: core.OutcomeToolCalls, ToolCalls: []core.ToolCall{{ID: "act-" + strconv.Itoa(m.asked), Name: "browser_act", Arguments: args}}}, nil
}

func TestEveryPageAForkedRunReachedIsVisitedFromItsRows(t *testing.T) {
	var rows []loop.Row
	acts := 0
	last, err := loop.Run(context.Background(), loop.Config{Model: &actsPastTheFactSheet{}, Spend: loop.SpendAPIKey, Tools: loop.NewRegistry(searchPage{acts: &acts}),
		Task: "find a house in Atibaia", ResultBytesCap: 4096, ArtifactDir: t.TempDir(), Budget: recall.Budget{Bands: recall.Bands{Recent: 8000}},
		EndedSession: func(ended loop.Row) error { rows = append(rows, ended); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || acts <= konst.FactSheetLines {
		t.Fatalf("the run forked %d times over %d acts and ended %s %q, want forks and more acts than the fact sheet holds", len(rows), acts, last.Outcome, last.ForkStop)
	}
	var visited []string
	for _, round := range append(rows, last) {
		for _, message := range round.Conversation {
			visited = append(visited, Visited(message.Content)...)
		}
	}
	for act := 1; act <= acts; act++ {
		if page := "https://www.airbnb.com.br/s/Atibaia--SP/homes?min_bedrooms=" + strconv.Itoa(act); !slices.Contains(visited, page) {
			t.Errorf("after %d forks the rows never show %s, only %v", len(rows), page, visited)
		}
	}
}
