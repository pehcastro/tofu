package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const corpusFileMode = 0o644

var pinField = regexp.MustCompile(`"line":(\d+)(,"fingerprint":"[0-9a-f]+")?`)

func fingerprintOnDisk(p Pin, tree string) (LineFingerprint, error) {
	lines, err := readLines(filepath.Join(TreeRoot(treeRootFromCorpus, tree), p.File))
	if err != nil {
		return "", err
	}
	if p.Line > len(lines) {
		return "", fmt.Errorf("%s holds %d lines and the pin names %d", p.File, len(lines), p.Line)
	}
	return fingerprintOf(lines[p.Line-1]), nil
}

func TestWritePins(t *testing.T) {
	if os.Getenv("TOFU_TOOLS_PIN") != "1" {
		t.Skip("set TOFU_TOOLS_PIN=1 to record every answer line's fingerprint from the tree as it stands, which is how a person says an answer moved with its code")
	}
	qs, err := ReadQuestions(corpusPath)
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	raw, err := readLines(corpusPath)
	if err != nil {
		t.Fatalf("readLines: %v", err)
	}
	if len(raw) != len(qs) {
		t.Fatalf("the corpus holds %d lines and parsed to %d questions: a blank or folded line would put the rewrite on the wrong row", len(raw), len(qs))
	}
	written := 0
	for i, q := range qs {
		pins := q.Pins()
		taken := 0
		raw[i] = pinField.ReplaceAllStringFunc(raw[i], func(match string) string {
			if taken >= len(pins) {
				t.Errorf("%s carries more line fields in its json than it has pins: the rewrite would land on the wrong field", q.ID)
				return match
			}
			p := pins[taken]
			taken++
			if pinField.FindStringSubmatch(match)[1] != strconv.Itoa(p.Line) {
				t.Errorf("%s: line field %d reads %s and pin %d names line %d", q.ID, taken, match, taken, p.Line)
				return match
			}
			fingerprint, err := fingerprintOnDisk(p, q.Tree)
			if err != nil {
				t.Errorf("%s: %v", q.ID, err)
				return match
			}
			written++
			return fmt.Sprintf("%q:%d,%q:%q", "line", p.Line, "fingerprint", string(fingerprint))
		})
		if taken != len(pins) {
			t.Errorf("%s holds %d pins and its json carries %d line fields", q.ID, len(pins), taken)
		}
	}
	if t.Failed() {
		return
	}
	if err := os.WriteFile(corpusPath, []byte(strings.Join(raw, "\n")+"\n"), corpusFileMode); err != nil {
		t.Fatalf("writing the corpus: %v", err)
	}
	t.Logf("%d fingerprints written over %d questions", written, len(qs))
}
