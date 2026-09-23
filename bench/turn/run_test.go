package turn

import (
	"context"
	"encoding/json"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	iturn "tofu/internal/turn"
)

type modelThatOnlyEverReadsTheSameMissingFile struct{}

func (modelThatOnlyEverReadsTheSameMissingFile) Ask(_ context.Context, _ llm.Request) (llm.Decision, error) {
	return llm.Decision{
		Build:   "bench-stub",
		Outcome: llm.OutcomeToolCalls,
		ToolCalls: []llm.ToolCall{{
			ID:        "call",
			Name:      "read",
			Arguments: json.RawMessage(`{"path":"absent.txt"}`),
		}},
	}, nil
}

func TestABenchTurnRunsWithTheLoopGuardFromKonst(t *testing.T) {
	task := Task{
		Name:   "a turn that repeats itself",
		Prompt: "read absent.txt",
		Check:  func(string) (bool, string) { return false, "" },
	}

	result := runOne(context.Background(), modelThatOnlyEverReadsTheSameMissingFile{}, task, ArmLoop, loopMaxSteps, 1)

	if result.CallErr != "" {
		t.Fatalf("the bench turn failed before the guard could stop it: %s", result.CallErr)
	}
	if result.Row.Outcome != iturn.OutcomeLoopGuard {
		t.Fatalf("outcome = %s, want %s: the bench builds its caps without the loop guard, so a repeating turn runs past it",
			result.Row.Outcome, iturn.OutcomeLoopGuard)
	}
	if result.Row.Guard == nil || result.Row.Guard.Repeats != konst.TurnLoopGuardRepeats {
		t.Fatalf("guard record = %+v, want the same %d repeats the shipped binary uses", result.Row.Guard, konst.TurnLoopGuardRepeats)
	}
}
