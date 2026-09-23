package rule

import (
	"strings"
	"testing"
)

const humanHead = "id: probe\ndomain: general\nkind: human\nconcern: task_shaping\n"

func TestAHumanRuleThatDeclaresNoTextFailsToLoadNamingTheRule(t *testing.T) {
	_, err := parseRule([]byte(humanHead), "library/general/rules/probe@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a human rule carrying no text, and text is all a human rule has")
	}
	for _, want := range []string{"library/general/rules/probe@1.yaml", "probe", "human", "text"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

func TestAHumanRuleLoadsWithNoCheckerAndCarriesItsTextSeparateFromItsNotes(t *testing.T) {
	r, err := parseRule([]byte(humanHead+"text: cite the turn rather than paraphrase it\nnotes: moved here by TOFU-403\n"), "library/general/rules/probe@1.yaml")
	if err != nil {
		t.Fatalf("a human rule with text and no checker did not load: %v", err)
	}
	if r.Checker != "" {
		t.Fatalf("checker = %q, want a human rule to name none", r.Checker)
	}
	if r.Text != "cite the turn rather than paraphrase it" {
		t.Fatalf("text = %q", r.Text)
	}
	if r.Notes != "moved here by TOFU-403" {
		t.Fatalf("notes = %q, want what a maintainer reads to stay where it was", r.Notes)
	}
}

func TestEveryKindButHumanStillRefusesAMissingChecker(t *testing.T) {
	for _, kind := range []Kind{KindStructural, KindDecision} {
		body := "id: probe\ndomain: dev\nconcern: code_rules\nkind: " + string(kind) + "\n"
		if _, err := parseRule([]byte(body), "library/dev/rules/probe@1.yaml"); err == nil {
			t.Fatalf("a %s rule loaded with no checker", kind)
		}
		if _, err := parseRule([]byte(body+"checker: comments\n"), "library/dev/rules/probe@1.yaml"); err != nil {
			t.Fatalf("a %s rule naming a checker did not load: %v", kind, err)
		}
	}
}

func TestAKindOtherThanHumanMayCarryTextToo(t *testing.T) {
	r := parseProbe(t, "concern: code_rules\ntext: write no comment, not one\n")
	if r.Text != "write no comment, not one" {
		t.Fatalf("text = %q, want a structural rule to be allowed model-facing text", r.Text)
	}
}
