package recall_test

import (
	"strings"
	"testing"

	"tofu/internal/recall"
)

func TestThePersonsWordsSurviveEveryForkOnceEachAndWordForWord(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	correction := "never run git push\nnot even with --dry-run"
	cron := "keep six agents running"
	first, err := recall.DistilledCarry(store, recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Step: 0, Text: "fix the parser", Said: "fix the parser"},
		{Step: 1, Text: correction, Said: correction},
		{Step: 1, Text: cron, Said: cron},
		{Step: 2, Tool: "bash", SupersedeKey: `bash {"command":"go test"}`, Text: "ok"},
		{Step: 2, Text: cron, Said: cron},
		{Step: 3, Text: "the parser is fixed"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fix the parser", correction, cron}
	if strings.Join(first.Said, "|") != strings.Join(want, "|") {
		t.Fatalf("the first fork carries %q, want %q", first.Said, want)
	}
	second, err := recall.DistilledCarry(store, recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Step: 0, Text: "the report of go-dev-2"},
		{Step: 0, Text: first.Text},
		{Step: 1, Text: "now the lexer", Said: "now the lexer"},
		{Step: 2, Text: "said: \"an assistant line is not the person\""},
	}})
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

func TestThePersonsWordsKeepTheNewestWithinTheCap(t *testing.T) {
	long := strings.Repeat("a", 40000)
	carry, err := recall.DistilledCarry(recall.NewStore(t.TempDir()), recall.Config{}, recall.Conversation{Entries: []recall.Entry{
		{Text: "the oldest", Said: "the oldest"},
		{Text: long, Said: long},
		{Text: "the newest", Said: "the newest"},
	}})
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
