package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

func fixedResultConfig(model Model, caps Caps) Config {
	config := cappedConfig(model, caps)
	config.Tools = NewRegistry(&stubTool{name: "noop", result: Result{Content: "ok"}})
	return config
}

func TestARepeatedCallWithTheSameResultStopsTheTurnPastTheLimit(t *testing.T) {
	model := alwaysToolCallModel(konst.TurnLoopGuardRepeats)
	model.decisions = append(model.decisions, messageDecision())

	row, err := Run(context.Background(), fixedResultConfig(model, Caps{MaxSteps: 10}))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeLoopGuard {
		t.Fatalf("outcome = %s, want the guard to stop the turn with its own outcome", row.Outcome)
	}
	if len(row.Steps) != konst.TurnLoopGuardRepeats+1 {
		t.Fatalf("expected %d repeated steps and one last word, got %d steps", konst.TurnLoopGuardRepeats+1, len(row.Steps))
	}
	if model.calls != konst.TurnLoopGuardRepeats+1 {
		t.Fatalf("expected the model asked %d times, the repeats plus one last word, got %d", konst.TurnLoopGuardRepeats+1, model.calls)
	}
}

func TestARepeatedCallCarriesADistinctOutcomeAndAReadableReason(t *testing.T) {
	model := alwaysToolCallModel(konst.TurnLoopGuardRepeats)
	model.decisions = append(model.decisions, messageDecision())

	row, err := Run(context.Background(), fixedResultConfig(model, Caps{MaxSteps: 10}))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Guard == nil {
		t.Fatal("a turn the guard stopped carries no record of why")
	}
	if row.Guard.Tool != "noop" || row.Guard.Repeats != konst.TurnLoopGuardRepeats {
		t.Fatalf("guard record = %+v, want tool noop repeated %d times", row.Guard, konst.TurnLoopGuardRepeats)
	}
	if len(row.Warnings) == 0 {
		t.Fatal("a turn the guard stopped carries no warning a person can read")
	}
	found := false
	for _, warning := range row.Warnings {
		if strings.Contains(warning, "noop") && strings.Contains(warning, "same result") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want one naming the tool and the repeat", row.Warnings)
	}
}

func TestTheSameCallReturningADifferentResultDoesNotTripTheGuard(t *testing.T) {
	decisions := make([]llm.Decision, konst.TurnLoopGuardRepeats+2)
	for i := range decisions {
		decisions[i] = toolCallDecision(llm.ToolCall{ID: "call", Name: "noop", Arguments: json.RawMessage(`{}`)})
	}
	model := &stubModel{decisions: append(decisions, messageDecision())}
	config := cappedConfig(model, Caps{MaxSteps: 10})
	config.Tools = NewRegistry(&stubTool{name: "noop", varying: true})

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Guard != nil {
		t.Fatalf("the guard tripped on a call whose result changed every time: %+v", row.Guard)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("outcome = %s, want the turn to finish on the model's own answer", row.Outcome)
	}
	if len(row.Steps) != len(decisions)+1 {
		t.Fatalf("expected every call to run, got %d of %d steps", len(row.Steps), len(decisions)+1)
	}
}

func TestTheSameFileReadTwiceFarApartDoesNotTripTheGuard(t *testing.T) {
	call := func(path string) llm.ToolCall {
		return llm.ToolCall{ID: "call", Name: "read", Arguments: json.RawMessage(`{"path":"` + path + `"}`)}
	}
	between := func() []llm.Decision {
		var decisions []llm.Decision
		for i := 0; i < konst.TurnLoopGuardWindow+1; i++ {
			decisions = append(decisions, toolCallDecision(call("other"+strconv.Itoa(i)+".txt")))
		}
		return decisions
	}
	var decisions []llm.Decision
	decisions = append(decisions, toolCallDecision(call("a.txt")))
	decisions = append(decisions, between()...)
	decisions = append(decisions, toolCallDecision(call("a.txt")))
	decisions = append(decisions, between()...)
	decisions = append(decisions, toolCallDecision(call("a.txt")))
	model := &stubModel{decisions: append(decisions, messageDecision())}

	config := cappedConfig(model, Caps{MaxSteps: len(decisions) + 2})
	config.Tools = NewRegistry(&stubTool{name: "read", result: Result{Content: "the same contents every time"}})

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Guard != nil {
		t.Fatalf("reading a.txt three times, each separated by %d other calls, tripped the guard: %+v", konst.TurnLoopGuardWindow+1, row.Guard)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("outcome = %s, want the turn to finish on the model's own answer", row.Outcome)
	}
}
