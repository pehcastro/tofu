package questions_test

import (
	"testing"

	"tofu/internal/browser"
	"tofu/internal/judge/question"
	"tofu/library/questions"
)

func TestBrowserStepOffersEveryOpByItsEnumName(t *testing.T) {
	set, _, err := question.Resolve("browser_step@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	operation, ok := set.Question("operation")
	if !ok {
		t.Fatal("the set asks no operation question")
	}
	offered := map[browser.Op]bool{}
	for _, option := range operation.Options {
		op, err := browser.ParseOp(option.Name)
		if err != nil {
			t.Errorf("option %q: %v", option.Name, err)
		}
		offered[op] = true
	}
	for op := browser.OpClick; op <= browser.OpBlocked; op++ {
		if !offered[op] {
			t.Errorf("the operation question does not offer %s", op)
		}
	}
	for _, finding := range question.Lint(set, question.DefaultCaps()) {
		perPage := finding.Question == "target" && (finding.Rule == question.RuleTooFewOptions || finding.Rule == question.RuleNoEscape)
		if !perPage {
			t.Errorf("lint: %s", finding)
		}
	}
}
