package library_test

import (
	"strings"
	"testing"

	"tofu/internal/rule"
	"tofu/library"
)

func shippedRule(t *testing.T, id string) rule.Rule {
	t.Helper()
	rules, err := rule.LoadFS(library.Files(), "library")
	if err != nil {
		t.Fatalf("loading the shipped rules: %v", err)
	}
	for _, one := range rules {
		if one.ID == id {
			return one
		}
	}
	t.Fatalf("the shipped rules carry no %s: %+v", id, rules)
	return rule.Rule{}
}

func TestTheQuoteRuleFiresOnlyOnATaskCarryingAReference(t *testing.T) {
	quote := shippedRule(t, "quote")
	if quote.Concern != rule.ConcernTaskShaping {
		t.Fatalf("concern = %q, want %q", quote.Concern, rule.ConcernTaskShaping)
	}
	if quote.Trigger.AlwaysOn() {
		t.Fatal("the quote rule is always on, and it costs its words on every turn that carries no reference")
	}
	withReference := rule.Index([]rule.Rule{quote}, rule.Task{Text: "what did you mean in [quote#39cl]"})
	if !withReference[0].Fires || !strings.Contains(withReference[0].Why, "[quote#39cl]") {
		t.Fatalf("a task carrying a reference did not fire it: %+v", withReference[0])
	}
	without := rule.Index([]rule.Rule{quote}, rule.Task{Text: "what did you mean earlier"})
	if without[0].Fires {
		t.Fatalf("a task carrying no reference fired it: %+v", without[0])
	}
}

func TestTheQuoteRuleSaysWhatEveryOutcomeOfResolvingAnIDIs(t *testing.T) {
	quote := shippedRule(t, "quote")
	for _, said := range []string{"read that turn before answering", "matching no turn, or more than one", "before event ids existed is quotable"} {
		if !strings.Contains(quote.Text, said) {
			t.Fatalf("the rule text does not say %q: %q", said, quote.Text)
		}
	}
	if quote.Notes != "" {
		t.Fatalf("notes = %q, and what the model reads is text rather than a maintainer's note", quote.Notes)
	}
}

func TestNoShippedHumanRuleNamesACheckerThatChecksNothing(t *testing.T) {
	rules, err := rule.LoadFS(library.Files(), "library")
	if err != nil {
		t.Fatalf("loading the shipped rules: %v", err)
	}
	human := 0
	for _, one := range rules {
		if one.Kind != rule.KindHuman {
			continue
		}
		human++
		if one.Checker != "" {
			t.Fatalf("the human rule %s names the checker %q, and no builtin checks a human rule", one.ID, one.Checker)
		}
	}
	if human == 0 {
		t.Fatal("no shipped rule is kind human, so this test proves nothing")
	}
}
