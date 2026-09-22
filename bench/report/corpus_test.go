package report

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryBenchCorpusHasADatedReport(t *testing.T) {
	if os.Getenv("TOFU_SWEEP_CORPUS_REPORTS") != "1" {
		t.Skip("set TOFU_SWEEP_CORPUS_REPORTS=1: this walks every package under bench/, not only this one, and a ticket mid-flight elsewhere would fail this package's build by default")
	}
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("reading bench: %v", err)
	}
	var missing []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkg := entry.Name()
		if !hasCorpus(filepath.Join("..", pkg, "testdata")) {
			continue
		}
		if !hasDatedReport(filepath.Join("..", pkg)) {
			missing = append(missing, pkg)
		}
	}
	if len(missing) > 0 {
		t.Errorf("bench packages with a corpus in testdata and no dated report of their own: %s", strings.Join(missing, ", "))
	}
}

func hasCorpus(testdataPath string) bool {
	found := false
	_ = filepath.WalkDir(testdataPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.EqualFold(entry.Name(), "PROVENANCE.md") {
			return err
		}
		found = true
		return nil
	})
	return found
}

func hasDatedReport(pkgPath string) bool {
	entries, err := os.ReadDir(pkgPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && datedReport.MatchString(entry.Name()) {
			return true
		}
	}
	return false
}
