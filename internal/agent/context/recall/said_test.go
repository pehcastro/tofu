package recall_test

import (
	"strings"
	"testing"

	"tofu/internal/agent/context/recall"
)

func TestThePersonsWordsSurviveEveryForkOnceEachAndWordForWord(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	correction := "never run git push\nnot even with --dry-run"
	cron := "keep six agents running"
	first, err := recall.CarryWithFiles(store, recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Step: 0, Text: "fix the parser", Said: "fix the parser"},
		{Step: 1, Text: correction, Said: correction},
		{Step: 1, Text: cron, Said: cron},
		{Step: 2, Tool: "bash", SupersedeKey: `bash {"command":"go test"}`, Text: "ok"},
		{Step: 2, Text: cron, Said: cron},
		{Step: 3, Text: "the parser is fixed"},
	}}, recall.WordsAlone, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fix the parser", correction, cron}
	if strings.Join(first.Said, "|") != strings.Join(want, "|") {
		t.Fatalf("the first fork carries %q, want %q", first.Said, want)
	}
	second, err := recall.CarryWithFiles(store, recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Step: 0, Text: "the report of go-dev-2"},
		{Step: 0, Text: first.Text},
		{Step: 1, Text: "now the lexer", Said: "now the lexer"},
		{Step: 2, Text: "said: \"an assistant line is not the person\""},
	}}, recall.WordsAlone, 0)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, "now the lexer")
	if strings.Join(second.Said, "|") != strings.Join(want, "|") {
		t.Fatalf("the second fork carries %q, want the three from the first fork and the new one, %q", second.Said, want)
	}
	if !strings.Contains(second.Text, recall.SaidCarryHeading) {
		t.Fatalf("the carry text has no section for the person's words:\n%s", second.Text)
	}
}

func TestTheStandingJudgeChoosesTheOrdersAndACarriedOrderIsNotAskedAgain(t *testing.T) {
	orders := map[string]bool{"Make every reply short.": true, "Use tabs in this repo.": true}
	var asked []string
	judge := func(sentences []string) []bool {
		asked = append(asked, sentences...)
		kept := make([]bool, len(sentences))
		for i, sentence := range sentences {
			kept[i] = orders[sentence]
		}
		return kept
	}
	carry, err := recall.CarryWithFiles(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Text: "an earlier fork\n" + recall.SaidCarryHeading + "the standing orders the person gave in this line of sessions:\norder: \"Never print the key.\"\n"},
		{Text: "a", Said: "Make every reply short. Do not worry about it."},
		{Text: "b", Said: "Use tabs in this repo."},
	}}, judge, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Never print the key.", "Make every reply short.", "Use tabs in this repo."}
	if strings.Join(carry.Standing, "|") != strings.Join(want, "|") {
		t.Fatalf("the carry keeps %q as standing orders, want %q", carry.Standing, want)
	}
	if wantAsked := []string{"Make every reply short.", "Do not worry about it.", "Use tabs in this repo."}; strings.Join(asked, "|") != strings.Join(wantAsked, "|") {
		t.Fatalf("the judge was asked %q, want each new sentence once and no carried order, %q", asked, wantAsked)
	}
}

func TestThePersonsWordsKeepTheNewestWithinTheCap(t *testing.T) {
	long := strings.Repeat("a", 40000)
	carry, err := recall.CarryWithFiles(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Text: "the oldest", Said: "the oldest"},
		{Text: long, Said: long},
		{Text: "the newest", Said: "the newest"},
	}}, recall.WordsAlone, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(carry.Said) == 0 || carry.Said[len(carry.Said)-1] != "the newest" {
		t.Fatalf("the newest message is not the last carried: %q", carry.Said)
	}
	for _, said := range carry.Said {
		if said == long {
			t.Fatalf("a %d byte message was carried whole", len(long))
		}
	}
}
