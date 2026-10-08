package recipe

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/turn"
)

type searchPage struct{ acts *int }

func (searchPage) Name() string { return "browser_act" }

func (searchPage) Definition() llm.Tool {
	return llm.Tool{Name: "browser_act", Parameters: map[string]any{"type": "object"}}
}

func (s searchPage) Run(context.Context, json.RawMessage) (turn.Result, error) {
	*s.acts++
	return turn.Result{Content: "1. click e7 \"Apply\": the page changed\n\n<<<ab12 begins>>>\ntab 1 https://www.airbnb.com.br/s/Atibaia--SP/homes?min_bedrooms=" + strconv.Itoa(*s.acts) + "\n- main\n<<<ab12 ends>>>"}, nil
}

type actsPastTheFactSheet struct{ asked int }

func (m *actsPastTheFactSheet) Ask(ctx context.Context, _ llm.Request) (llm.Decision, error) {
	if turn.AskedQuietly(ctx) {
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "goal: a house in Atibaia"}, nil
	}
	m.asked++
	if m.asked > konst.FactSheetLines+2 {
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	args := json.RawMessage(`{"tab":1,"actions":[{"action":"click","ref":"e7"}]}`)
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{{ID: "act-" + strconv.Itoa(m.asked), Name: "browser_act", Arguments: args}}}, nil
}

func TestEveryPageAForkedRunReachedIsVisitedFromItsRows(t *testing.T) {
	var rows []turn.Row
	acts := 0
	last, err := turn.Run(context.Background(), turn.Config{Model: &actsPastTheFactSheet{}, Spend: turn.SpendAPIKey, Tools: turn.NewRegistry(searchPage{acts: &acts}),
		Task: "find a house in Atibaia", ResultBytesCap: 4096, ArtifactDir: t.TempDir(), Budget: recall.Budget{Bands: recall.Bands{Recent: 1}},
		EndedSession: func(ended turn.Row) error { rows = append(rows, ended); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || acts <= konst.FactSheetLines {
		t.Fatalf("the run forked %d times over %d acts, want a fork after every act", len(rows), acts)
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
