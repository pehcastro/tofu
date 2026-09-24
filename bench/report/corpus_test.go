package report

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryBenchCorpusHasADatedReport(t *testing.T) {
	declared, err := readPackages(filepath.Join("..", "report", "packages.json"))
	if err != nil {
		t.Fatalf("reading the declared packages: %v", err)
	}
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("reading bench: %v", err)
	}
	var missing, owed []string
	directories, withTestdata, withCorpus := 0, 0, 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directories++
		pkg := entry.Name()
		inTestdata := testdataHoldsCorpus(filepath.Join("..", pkg, "testdata"))
		if inTestdata {
			withTestdata++
		}
		if !inTestdata && !readsRecordedSessions(filepath.Join("..", pkg)) {
			continue
		}
		withCorpus++
		if hasDatedReport(filepath.Join("..", pkg)) {
			continue
		}
		owes, named := declared[pkg]
		if !named || owes.Kind != KindUnpublished {
			missing = append(missing, pkg)
			continue
		}
		owed = append(owed, pkg+": "+owes.Note)
	}
	t.Logf("%d of %d directories under bench/ hold a corpus, %d of them by a testdata directory, which was the whole of the rule this sweep read before", withCorpus, directories, withTestdata)
	if len(owed) > 0 {
		t.Logf("with a corpus, no dated report, and declared in %s as owing one: %s", PackagesPath, strings.Join(owed, "; "))
	}
	if len(missing) > 0 {
		t.Errorf("bench packages that read a corpus, have no dated report and are not declared in %s as owing one: %s", PackagesPath, strings.Join(missing, ", "))
	}
}

func testdataHoldsCorpus(testdataPath string) bool {
	return anyFile(testdataPath, func(_, name string) bool {
		return !strings.EqualFold(name, "PROVENANCE.md")
	})
}

func readsRecordedSessions(pkgPath string) bool {
	return anyFile(pkgPath, func(path, name string) bool {
		if !strings.HasSuffix(name, ".go") {
			return false
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return false
		}
		for _, imported := range file.Imports {
			if imported.Path.Value == `"tofu/bench/corpus"` {
				return true
			}
		}
		return false
	})
}

func anyFile(root string, matches func(path, name string) bool) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found {
			return err
		}
		found = matches(path, entry.Name())
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
