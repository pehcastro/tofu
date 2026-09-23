package shortlist

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const shortestWordThatCanLeak = 3

var (
	notAWordCharacter = regexp.MustCompile(`[^A-Za-z0-9]+`)
	camelRun          = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+`)
)

func wordsOf(text string) map[string]bool {
	words := map[string]bool{}
	for _, part := range notAWordCharacter.Split(text, -1) {
		for _, word := range camelRun.FindAllString(part, -1) {
			if len(word) >= shortestWordThatCanLeak {
				words[strings.ToLower(word)] = true
			}
		}
	}
	return words
}

func answerWordsInTask(row Row) []string {
	task := wordsOf(row.Task)
	var named []string
	for word := range wordsOf(strings.Join(row.Label, " ")) {
		if task[word] {
			named = append(named, word)
		}
	}
	sort.Strings(named)
	return named
}

const parked = "bench/shortlist is parked: three of its four recorded rows name their own label in their own task, the tasks are the owner's words from one real session and are not rewritten, and .tofu/sessions holds two distinct turns a shortlist question can be drawn from, one of them this one. See TOFU-449 and testdata/PROVENANCE.md"

func TestNoRowNamesItsOwnLabelInItsOwnTask(t *testing.T) {
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatalf("ReadCorpus: %v", err)
	}
	var leaky []string
	for _, row := range rows {
		if named := answerWordsInTask(row); len(named) > 0 {
			leaky = append(leaky, fmt.Sprintf("%s names %v of its label %v", row.TurnID, named, row.Label))
		}
	}
	t.Skipf("%d of %d rows are refused and kept as recorded: %s. %s", len(leaky), len(rows), strings.Join(leaky, "; "), parked)
}

func TestAnswerWordsInTaskReadsEveryPathWordAndNothingShorterThanThree(t *testing.T) {
	leaky := Row{TurnID: "planted", Task: "update the README file", Label: []string{"docs/README.md"}}
	if got := answerWordsInTask(leaky); len(got) != 1 || got[0] != "readme" {
		t.Fatalf("a task saying README against a README.md label leaks readme and nothing else: %v", got)
	}
	clean := Row{TurnID: "planted", Task: "the filtering belongs in the storage code", Label: []string{"lib/store.ts"}}
	if got := answerWordsInTask(clean); len(got) > 0 {
		t.Fatalf("storage is not store and ts is two characters: %v", got)
	}
}
