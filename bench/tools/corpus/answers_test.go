package corpus

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/bench/tools"
)

const (
	corpusPath         = "../testdata/questions.jsonl"
	treeRootFromCorpus = "../../.."
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
	qs, err := ReadQuestions(corpusPath)
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	absent := map[string]int{}
	checked, unanswerable, moved, unpinned, unanchored := 0, 0, 0, 0, 0
	for _, q := range qs {
		root := TreeRoot(treeRootFromCorpus, q.Tree)
		if _, err := os.Stat(root); err != nil {
			absent[q.Tree]++
			continue
		}
		for _, p := range q.Pins() {
			checked++
			lines, err := readLines(filepath.Join(root, p.File))
			if err != nil {
				unanswerable++
				t.Errorf("%s answers %s:%d and that file is not on disk: every arm scores zero on it and the miss measures the corpus", q.ID, p.File, p.Line)
				continue
			}
			if p.Line > len(lines) {
				unanswerable++
				t.Errorf("%s answers %s:%d and that file holds %d lines: the answer moved and the question can no longer be scored", q.ID, p.File, p.Line, len(lines))
				continue
			}
			line := lines[p.Line-1]
			switch now := fingerprintOf(line); {
			case p.Fingerprint == "":
				unpinned++
				t.Errorf("%s answers %s:%d with nothing fingerprinted beside the pin: that line can be rewritten under the corpus with no test noticing, so record it with TOFU_TOOLS_PIN=1", q.ID, p.File, p.Line)
			case now != p.Fingerprint:
				moved++
				where := "and no line in that file carries the recorded fingerprint any more"
				if at := slices.IndexFunc(lines, func(l string) bool { return fingerprintOf(l) == p.Fingerprint }); at >= 0 {
					where = fmt.Sprintf("and the line it pinned now sits at %d, reading %q", at+1, lines[at])
				}
				t.Errorf("%s answers %s:%d in the %s tree and that line changed under its pin: it fingerprinted %s when the pin was written, it now reads %q which fingerprints %s, %s. Decide whether the answer moved or the code did, then repin with TOFU_TOOLS_PIN=1",
					q.ID, p.File, p.Line, q.Tree, p.Fingerprint, line, now, where)
			}
			terms := tools.QuotedTerms(q.Text)
			if q.Band != BandNamed || len(terms) == 0 {
				unanchored++
				continue
			}
			if !slices.ContainsFunc(terms, func(term string) bool { return strings.Contains(line, term) }) {
				unanswerable++
				t.Errorf("%s answers %s:%d and that line carries none of its quoted terms %v: the tree moved under the pin", q.ID, p.File, p.Line, terms)
			}
		}
	}
	for _, tree := range slices.Sorted(maps.Keys(absent)) {
		t.Logf("%d questions not checked: the tree %s is not on this machine", absent[tree], tree)
	}
	t.Logf("%d answers checked, %d unanswerable, %d changed under a fingerprint, %d carrying no fingerprint, %d carrying no quoted term to anchor the pin, which is every described and intent question and the fingerprint is what now holds them", checked, unanswerable, moved, unpinned, unanchored)
}
