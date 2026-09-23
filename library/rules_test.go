package library_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/rule"
	"tofu/library"
)

func everyRuleFile(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := fs.WalkDir(library.Files(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Base(path.Dir(name)) != "rules" {
			return err
		}
		data, err := fs.ReadFile(library.Files(), name)
		if err != nil {
			return err
		}
		found[name] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the shipped rules: %v", err)
	}
	return found
}

func TestTheReadmeNamesEveryRuleThatShipsEnforced(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", ".local", "boji", "planning", "library.md"))
	if err != nil {
		t.Skipf("the library's written description is not on this machine: %v", err)
	}
	said := string(readme)
	enforced := 0
	for name, body := range everyRuleFile(t) {
		if !strings.Contains(body, "mode: enforced") {
			continue
		}
		enforced++
		id := strings.SplitN(path.Base(name), "@", 2)[0]
		if !strings.Contains(said, id) {
			t.Fatalf("%s ships mode: enforced and the README never names %s, so a reader has no way to learn what being enforced does for it", name, id)
		}
	}
	if enforced == 0 {
		t.Fatal("no shipped rule declares mode: enforced, so this test proves nothing")
	}
	t.Logf("%d shipped rule files declare mode: enforced", enforced)
}

func TestNoRuleWithoutACheckerCarriesAModeLine(t *testing.T) {
	checkerless := 0
	for name, body := range everyRuleFile(t) {
		if strings.Contains(body, "kind: "+rule.ThresholdKind) || strings.Contains(body, "checker:") {
			continue
		}
		checkerless++
		if strings.Contains(body, "mode:") {
			t.Fatalf("%s names no checker and declares a mode, and shadow against enforced decides nothing for a rule nothing checks", name)
		}
	}
	if checkerless == 0 {
		t.Fatal("every shipped rule names a checker or is a decision point, so this test proves nothing")
	}
	t.Logf("%d shipped rule files name no checker and carry no mode", checkerless)
}

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
