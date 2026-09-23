package corpus_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const benchRoot = ".."

const provenanceFile = "PROVENANCE.md"

var ownedRoots = []string{
	"api", "corpus", "cost", "forkcache", "mutate", "readworth",
	"recall", "shortlist", "stopcheck", "testquality", "tools", "transform",
}

var fixtureExtensions = []string{".json", ".jsonl", ".yaml", ".txt"}

var datedReportPrefixes = []string{"report-", "sweep-", "rescore-"}

func TestEveryFixtureDirectorySaysWhereItCameFrom(t *testing.T) {
	for _, root := range ownedRoots {
		start := filepath.Join(benchRoot, root)
		err := filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			holds, err := holdsFixtures(path)
			if err != nil || !holds || coveredByProvenance(path, start) {
				return err
			}
			t.Errorf("%s holds fixtures and carries no %s saying whether they were recorded or written", filepath.ToSlash(path), provenanceFile)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func holdsFixtures(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	underTestdata := slices.Contains(strings.Split(filepath.ToSlash(dir), "/"), "testdata")
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == provenanceFile {
			continue
		}
		if underTestdata || isFixtureFile(entry.Name()) {
			return true, nil
		}
	}
	return false, nil
}

func isFixtureFile(name string) bool {
	for _, prefix := range datedReportPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	for _, extension := range fixtureExtensions {
		if strings.HasSuffix(name, extension) {
			return true
		}
	}
	return false
}

func coveredByProvenance(dir, stopAt string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, provenanceFile)); err == nil {
			return true
		}
		if dir == stopAt {
			return false
		}
		dir = filepath.Dir(dir)
	}
}
