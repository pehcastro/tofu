package turn

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"boji/internal/llm"
)

func alwaysToolCallModel(cost float64, n int) *stubModel {
	decisions := make([]llm.Decision, n)
	for i := range decisions {
		decisions[i] = llm.Decision{
			Build:   "m1",
			Outcome: llm.OutcomeToolCalls,
			Usage:   llm.Usage{Cost: cost},
			ToolCalls: []llm.ToolCall{
				{ID: "call", Name: "noop", Arguments: json.RawMessage(`{}`)},
			},
		}
	}
	return &stubModel{decisions: decisions}
}

func TestRunStopsAtTheStepCapAndRecordsIt(t *testing.T) {
	tool := &stubTool{name: "noop", result: Result{Content: "ok"}}
	model := alwaysToolCallModel(0, 10)
	config := Config{
		Model:          model,
		Tools:          NewRegistry(tool),
		Task:           "loop forever",
		Caps:           Caps{MaxSteps: 3},
		ResultBytesCap: 4096,
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStepCap {
		t.Fatalf("expected outcome step_cap, got %s", row.Outcome)
	}
	if len(row.Steps) != 3 {
		t.Fatalf("expected exactly 3 steps, got %d", len(row.Steps))
	}
}

func TestRunStopsAtTheCostCapAndRecordsIt(t *testing.T) {
	tool := &stubTool{name: "noop", result: Result{Content: "ok"}}
	model := alwaysToolCallModel(1.0, 10)
	config := Config{
		Model:          model,
		Tools:          NewRegistry(tool),
		Task:           "loop forever",
		Caps:           Caps{MaxCostUSD: 2.5},
		ResultBytesCap: 4096,
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeCostCap {
		t.Fatalf("expected outcome cost_cap, got %s", row.Outcome)
	}
	if len(row.Steps) != 3 {
		t.Fatalf("expected exactly 3 steps before the cap tripped, got %d", len(row.Steps))
	}
	if row.TotalCostUSD != 3.0 {
		t.Fatalf("expected total cost 3.0, got %v", row.TotalCostUSD)
	}
}

func TestRunStopsAtTheWallClockCapAndRecordsIt(t *testing.T) {
	tool := &stubTool{name: "noop", result: Result{Content: "ok"}}
	model := alwaysToolCallModel(0, 10)

	clock := time.Now()
	advance := func() time.Time {
		now := clock
		clock = clock.Add(time.Second)
		return now
	}

	config := Config{
		Model:          model,
		Tools:          NewRegistry(tool),
		Task:           "loop forever",
		Caps:           Caps{MaxWallClock: 3 * time.Second},
		ResultBytesCap: 4096,
		Now:            advance,
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeWallClockCap {
		t.Fatalf("expected outcome wall_clock_cap, got %s", row.Outcome)
	}
	if row.WallClockMS < 3000 {
		t.Fatalf("expected recorded wall clock at or past the cap, got %d ms", row.WallClockMS)
	}
}

func TestCapsAreEachIndependentlyOff(t *testing.T) {
	tool := &stubTool{name: "noop", result: Result{Content: "ok"}}
	model := alwaysToolCallModel(100, 2)
	model.decisions = append(model.decisions, messageDecision())
	config := Config{
		Model:          model,
		Tools:          NewRegistry(tool),
		Task:           "no caps but the step cap",
		Caps:           Caps{MaxSteps: 3},
		ResultBytesCap: 4096,
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected a cost of 200 with no cost cap to still reach stopped, got %s", row.Outcome)
	}
}
