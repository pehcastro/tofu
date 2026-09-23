package prefix

import (
	"testing"

	"tofu/internal/rule"
)

func realRules(t *testing.T) []rule.Rule {
	t.Helper()
	rules, err := RealRules()
	if err != nil {
		t.Fatalf("loading the real rule library: %v", err)
	}
	return rules
}

func composedSystem(t *testing.T, rules []rule.Rule) (string, []string) {
	t.Helper()
	system, composed, err := ComposeSystem(rules)
	if err != nil {
		t.Fatalf("composing the system prompt: %v", err)
	}
	return system, FiredRuleIDs(composed)
}

func measured(t *testing.T, system string, oauth bool) RewriteFigure {
	t.Helper()
	figure, err := MeasureRewrite(system, oauth)
	if err != nil {
		t.Fatalf("measuring: %v", err)
	}
	return figure
}

func TestRealComposedPromptFiresRules(t *testing.T) {
	_, fired := composedSystem(t, realRules(t))
	if len(fired) == 0 {
		t.Fatalf("the task %q fired no rule at all, so this measures the same floor TOFU-532 already has", RealTask)
	}
	t.Logf("%d rules fired for %q: %v", len(fired), RealTask, fired)
}

func TestRewritePerSessionWithTheBillingBlock(t *testing.T) {
	system, _ := composedSystem(t, realRules(t))
	figure := measured(t, system, true)
	if !figure.Rewritten {
		t.Fatalf("the billing block is present and the system array did not vary between two fingerprinted first messages")
	}
	t.Logf("with the billing block: %d bytes, %d estimated tokens rewritten per session", figure.Bytes, figure.Tokens)
}

func TestRewritePerSessionWithTheBillingBlockAbsent(t *testing.T) {
	system, _ := composedSystem(t, realRules(t))
	figure := measured(t, system, false)
	if figure.Rewritten {
		t.Fatalf("the billing block is absent and the system array still varied between two fingerprinted first messages: %d bytes", figure.Bytes)
	}
	t.Logf("with the billing block absent: %d bytes total, 0 rewritten per session", figure.Bytes)
}

func TestBuiltinOnlySystemIsolatesTheRulesShareOfTheGap(t *testing.T) {
	full, _ := composedSystem(t, realRules(t))
	builtinOnly, _ := composedSystem(t, nil)
	fullFigure := measured(t, full, true)
	builtinFigure := measured(t, builtinOnly, true)
	if fullFigure.Bytes <= builtinFigure.Bytes {
		t.Fatalf("the rules that fired added no bytes: full %d, builtin only %d", fullFigure.Bytes, builtinFigure.Bytes)
	}
	t.Logf("full %d bytes, builtin only %d bytes, rules added %d bytes", fullFigure.Bytes, builtinFigure.Bytes, fullFigure.Bytes-builtinFigure.Bytes)
}

func TestGeneratorReproducesTheSameBytesTwice(t *testing.T) {
	rules := realRules(t)
	system1, _ := composedSystem(t, rules)
	system2, _ := composedSystem(t, rules)
	if system1 != system2 {
		t.Fatalf("the same rules composed twice produced different system text")
	}
	first := measured(t, system1, true)
	second := measured(t, system2, true)
	if first != second {
		t.Fatalf("the generator run twice gave different figures: %+v vs %+v", first, second)
	}
	t.Logf("run one: %d bytes, %d tokens. run two: %d bytes, %d tokens. identical", first.Bytes, first.Tokens, second.Bytes, second.Tokens)
}
