package corpus

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/bench/tools"
)

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func TestEveryAnswerStillResolvesOnDisk(t *testing.T) {
	qs, err := ReadQuestions("../testdata/questions.jsonl")
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	absent := map[string]int{}
	checked, unanswerable, unanchored := 0, 0, 0
	for _, q := range qs {
		root := TreeRoot("../../..", q.Tree)
		if _, err := os.Stat(root); err != nil {
			absent[q.Tree]++
			continue
		}
		for _, a := range q.AllAnswers() {
			checked++
			path := filepath.Join(root, a.File)
			lines, err := readLines(path)
			if err != nil {
				unanswerable++
				t.Errorf("%s answers %s:%d and that file is not on disk: every arm scores zero on it and the miss measures the corpus", q.ID, a.File, a.Line)
				continue
			}
			if a.Line > len(lines) {
				unanswerable++
				t.Errorf("%s answers %s:%d and that file holds %d lines: the answer moved and the question can no longer be scored", q.ID, a.File, a.Line, len(lines))
				continue
			}
			terms := tools.QuotedTerms(q.Text)
			if q.Band != BandNamed || len(terms) == 0 {
				unanchored++
				continue
			}
			line := lines[a.Line-1]
			if !slices.ContainsFunc(terms, func(term string) bool { return strings.Contains(line, term) }) {
				unanswerable++
				t.Errorf("%s answers %s:%d and that line carries none of its quoted terms %v: the tree moved under the pin", q.ID, a.File, a.Line, terms)
			}
		}
	}
	for _, tree := range slices.Sorted(maps.Keys(absent)) {
		t.Logf("%d questions not checked: the tree %s is not on this machine", absent[tree], tree)
	}
	t.Logf("%d answers checked, %d unanswerable, %d carrying no quoted term to anchor the pin, which is every described and intent question and they can rot without this test noticing", checked, unanswerable, unanchored)
}
