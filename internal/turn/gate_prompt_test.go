package turn

import (
	"context"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/settings"
)

func TestGateModeFromPromptMapsTheTwoNamedValues(t *testing.T) {
	if GateModeFromPrompt(settings.GatePromptRun) != GateShadow {
		t.Fatalf("GateModeFromPrompt(%q) did not map to GateShadow", settings.GatePromptRun)
	}
	if GateModeFromPrompt(settings.GatePromptAsk) != GateEnforce {
		t.Fatalf("GateModeFromPrompt(%q) did not map to GateEnforce", settings.GatePromptAsk)
	}
	if GateModeFromPrompt("nonsense") != GateShadow {
		t.Fatal("an unknown value must fail to the quiet mode, not the interrupting one")
	}
}

func TestTheShippedDefaultRunsAToolCallWithoutAsking(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := callThenAnswer(judgedWrite("c1", "a.txt"))
	config := gatedConfig(t, gateSaying(ledger.VerdictAsk), tool, model)
	config.GateMode = GateModeFromPrompt(settings.GatePromptRun)
	asked := 0
	config.Person = func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
		asked++
		return PersonDenied, nil
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if asked != 0 {
		t.Fatalf("the shipped default asked the person %d times, want zero", asked)
	}
	if tool.calls != 1 {
		t.Fatalf("the shipped default ran the tool %d times, want one", tool.calls)
	}
	if got := row.Steps[0].ToolCalls[0].GateVerdict; got != string(ledger.VerdictAsk) {
		t.Fatalf("the tool call row carries verdict %q, want %q recorded even though nobody was asked", got, ledger.VerdictAsk)
	}
}

func TestTheAskingModePromptsAndWritesGateAnswer(t *testing.T) {
	tool := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := callThenAnswer(judgedWrite("c1", "a.txt"))
	config := gatedConfig(t, gateSaying(ledger.VerdictAsk), tool, model)
	config.GateMode = GateModeFromPrompt(settings.GatePromptAsk)
	asked := 0
	var answered PersonAnswer
	config.Person = func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
		asked++
		answered = PersonAllowedOnce
		return answered, nil
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if asked != 1 {
		t.Fatalf("the asking mode asked the person %d times, want one", asked)
	}
	if tool.calls != 1 {
		t.Fatalf("the person allowed the call and the tool ran %d times, want one", tool.calls)
	}
	if outcome := answered.Outcome(); outcome.Kind != OutcomeKindGateAnswer {
		t.Fatalf("the person's answer carries outcome kind %q, want %q", outcome.Kind, OutcomeKindGateAnswer)
	}
	if row.Steps[0].ToolCalls[0].GateDecisionID == "" {
		t.Fatalf("the tool call row carries no decision id for %q to be written onto: %+v", OutcomeKindGateAnswer, row.Steps[0].ToolCalls[0])
	}
}

func TestADecisionIsRecordedInEveryPromptMode(t *testing.T) {
	for _, prompt := range []string{settings.GatePromptRun, settings.GatePromptAsk} {
		tool := &stubTool{name: "write", result: Result{Content: "ok"}}
		model := callThenAnswer(judgedWrite("c1", "a.txt"))
		gate := gateSaying(ledger.VerdictAllow)
		config := gatedConfig(t, gate, tool, model)
		config.GateMode = GateModeFromPrompt(prompt)
		config.Person = func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
			return PersonAllowedOnce, nil
		}

		row, err := Run(context.Background(), config)
		if err != nil {
			t.Fatalf("prompt %q: Run returned an error: %v", prompt, err)
		}
		if len(gate.requests) != 1 {
			t.Fatalf("prompt %q: the gate was asked %d times, want one regardless of the setting", prompt, len(gate.requests))
		}
		if len(row.DecisionIDs) != 1 {
			t.Fatalf("prompt %q: the turn recorded %d decisions, want one", prompt, len(row.DecisionIDs))
		}
	}
}
