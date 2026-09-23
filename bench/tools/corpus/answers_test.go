package corpus

import (
	"bufio"
	"errors"
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
			if p.Fingerprint == "" {
				unpinned++
				t.Errorf("%s answers %s:%d with nothing fingerprinted beside the pin: that line can be rewritten under the corpus with no test noticing, so record it with TOFU_TOOLS_PIN=1", q.ID, p.File, p.Line)
				continue
			}
			resolution, err := ResolvePin(lines, p)
			if err != nil {
				switch {
				case errors.Is(err, ErrPinAmbiguous):
					unanswerable++
					t.Errorf("%s answers %s:%d and %v: refusing to guess which one, repin with TOFU_TOOLS_PIN=1 after picking one", q.ID, p.File, p.Line, err)
				default:
					moved++
					t.Errorf("%s answers %s:%d in the %s tree and %v: decide whether the answer moved or the code did, then repin with TOFU_TOOLS_PIN=1", q.ID, p.File, p.Line, q.Tree, err)
				}
				continue
			}
			if resolution.Moved {
				t.Logf("%s answers %s:%d and its fingerprint followed the shift to line %d unchanged", q.ID, p.File, p.Line, resolution.Line)
			}
			line := lines[resolution.Line-1]
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
