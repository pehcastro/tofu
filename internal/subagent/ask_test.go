package subagent

import (
	"errors"
	"testing"
)

func TestDecideAskRefusesAnUnknownAction(t *testing.T) {
	_, err := DecideAsk("escalate", 0.1, 0.35)
	var unknown UnknownActionError
	if !errors.As(err, &unknown) || unknown.Action != "escalate" {
		t.Fatalf("got %v, want an UnknownActionError naming escalate", err)
	}
}

func TestDecideAskRespectsTheChoiceOnlyBelowTheMargin(t *testing.T) {
	low, err := DecideAsk(ActionAskNow, 0.10, 0.35)
	if err != nil {
		t.Fatal(err)
	}
	if low.Effective != ActionAskNow {
		t.Fatalf("determined 0.10 under 0.35 gave %q, want %q", low.Effective, ActionAskNow)
	}

	high, err := DecideAsk(ActionAskNow, 0.90, 0.35)
	if err != nil {
		t.Fatal(err)
	}
	if high.Effective != ActionProceed {
		t.Fatalf("determined 0.90 over 0.35 gave %q, want the margin gate to force %q", high.Effective, ActionProceed)
	}

	atMargin, err := DecideAsk(ActionDefer, 0.35, 0.35)
	if err != nil {
		t.Fatal(err)
	}
	if atMargin.Effective != ActionProceed {
		t.Fatalf("determined exactly at the margin gave %q, want %q", atMargin.Effective, ActionProceed)
	}
}

func TestGateVerdictQuestionMapsEachEffectiveAction(t *testing.T) {
	state := AskState{Path: "internal/turn/loop.go"}

	proceed, _ := DecideAsk(ActionAskNow, 0.9, 0.35)
	if _, asked := proceed.Question(state); asked {
		t.Fatal("a proceed verdict recorded a question")
	}

	askNow, _ := DecideAsk(ActionAskNow, 0.1, 0.35)
	q, asked := askNow.Question(state)
	if !asked || q.Kind != Blocking || q.Where != state.Path {
		t.Fatalf("ask_now gave %+v, asked=%v", q, asked)
	}

	deferred, _ := DecideAsk(ActionDefer, 0.1, 0.35)
	q, asked = deferred.Question(state)
	if !asked || q.Kind != Deferred || q.Where != state.Path {
		t.Fatalf("defer_to_end gave %+v, asked=%v", q, asked)
	}
}

func TestTheAskGateIsShadowAndChangesNothingAWorkerIsToldAtTheBoundary(t *testing.T) {
	plain := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	plainErr := plain.Write("internal/turn/loop.go")

	gated := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	verdict, err := DecideAsk(ActionAskNow, 0.05, 0.35)
	if err != nil {
		t.Fatal(err)
	}
	state := AskState{Path: "internal/turn/loop.go", InOwns: false}
	question, asked := verdict.Question(state)
	if !asked {
		t.Fatal("a genuinely low determined score recorded no question")
	}
	gated.Ask(question)
	gatedErr := gated.Write("internal/turn/loop.go")

	if plainErr == nil || gatedErr == nil {
		t.Fatal("a write outside owns was allowed")
	}
	if plainErr.Error() != gatedErr.Error() {
		t.Fatalf("the shadow gate changed the refusal:\nwithout it: %v\nwith it:    %v", plainErr, gatedErr)
	}

	insideA := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	insideB := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	insideB.Ask(question)
	if errA, errB := insideA.Write("internal/subagent/ask.go"), insideB.Write("internal/subagent/ask.go"); errA != nil || errB != nil {
		t.Fatalf("a write inside owns was refused: without gate %v, with gate %v", errA, errB)
	}
}
