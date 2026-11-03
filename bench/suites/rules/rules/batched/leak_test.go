package main

import (
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func corpusAndRules(t *testing.T) ([]reply, []string, func([]reply) []string) {
	t.Helper()
	replies, err := loadCorpus(filepath.Join(sys.SourceRoot(), packageDir, "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := loadRules()
	if err != nil {
		t.Fatal(err)
	}
	reportShort, ok := findRule(rules, "report_short")
	if !ok {
		t.Fatal("the library has no report_short rule")
	}
	return replies, words(reportShort.Text), func(rs []reply) []string { return leaks(rs, rules) }
}

func TestTheRecordedCorpusQuotesNoRuleItIsLabelledOn(t *testing.T) {
	replies, _, check := corpusAndRules(t)
	if found := check(replies); len(found) > 0 {
		t.Fatalf("the corpus leaks:\n%s", strings.Join(found, "\n"))
	}
}

func TestAReplyQuotingItsOwnRuleTextIsCaught(t *testing.T) {
	replies, ruleWords, check := corpusAndRules(t)
	planted := reply{
		ID:      "planted",
		Request: "where are we?",
		Reply:   "Done. As the rule says, " + strings.ToUpper(strings.Join(ruleWords[10:10+leakRunOfWords], ", ")) + ", so here it is.",
		Labels:  map[string]bool{"report_short": false},
	}
	found := check(append(replies, planted))
	if len(found) != 1 || !strings.HasPrefix(found[0], "planted quotes report_short") {
		t.Fatalf("want one leak on the planted reply, got %q", found)
	}
}
