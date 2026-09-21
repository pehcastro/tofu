package corpus

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/bench/tools"
)

var identifierSplit = regexp.MustCompile(`[^A-Za-z0-9]+`)

func splitIdentifier(s string) []string {
	var out []string
	for _, part := range identifierSplit.Split(s, -1) {
		out = append(out, splitCamel(part)...)
	}
	return out
}

func splitCamel(s string) []string {
	var words []string
	var cur []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && cur[len(cur)-1] >= 'a' {
			words = append(words, string(cur))
			cur = nil
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

func forbiddenWords(a Answer, identifiers []string) map[string]bool {
	forbidden := map[string]bool{}
	add := func(s string) {
		for _, w := range splitIdentifier(s) {
			if len(w) >= 3 {
				forbidden[strings.ToLower(w)] = true
			}
		}
	}
	for _, part := range strings.Split(a.File, "/") {
		add(part)
		add(strings.TrimSuffix(part, filepath.Ext(part)))
	}
	for _, id := range identifiers {
		add(id)
	}
	return forbidden
}

func TestDescribedAndIntentQuestionsCarryNoAnswerVocabulary(t *testing.T) {
	qs, err := ReadQuestions("../testdata/questions.jsonl")
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	if len(qs) == 0 {
		t.Fatal("the corpus is empty")
	}
	for _, q := range qs {
		if q.Band == BandNamed {
			continue
		}
		words := tools.ContentWords(q.Text)
		for _, a := range q.AllAnswers() {
			forbidden := forbiddenWords(a, q.Identifiers)
			for _, w := range words {
				if forbidden[strings.ToLower(w)] {
					t.Errorf("%s (%s): %q names %q, a path component, stem or identifier of its own answer %s:%d",
						q.ID, q.Band, w, w, a.File, a.Line)
				}
			}
		}
	}
}
