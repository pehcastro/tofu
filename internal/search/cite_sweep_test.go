package search_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/search"
)

const sweepEnvar = "TOFU_CITE_SWEEP"

func TestEveryCitationInTheReportsUnderTheSweptDirectory(t *testing.T) {
	dir := os.Getenv(sweepEnvar)
	if dir == "" {
		t.Skip("skipped: " + sweepEnvar + " names no directory of reports to sweep, and this test reads no path of its own")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locating the repository: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	total, resolved := 0, 0
	var refused []string
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		cited := search.Cite(root, string(body))
		total, resolved = total+len(cited.Found), resolved+cited.Resolved
		for _, one := range cited.Found {
			if one.Claim != search.ClaimResolved {
				refused = append(refused, entry.Name()+"  "+one.Text+"  "+string(one.Claim)+": "+one.Detail)
			}
		}
	}
	if total == 0 {
		t.Fatalf("%d files under %s carry no citation at all, which is a finding rather than a pass", len(entries), dir)
	}

	slices.Sort(refused)
	t.Logf("%d files, %d citations, %d resolved, %d refused, against the tree at %s\n%s",
		len(entries), total, resolved, len(refused), root, strings.Join(slices.Compact(refused), "\n"))
}
