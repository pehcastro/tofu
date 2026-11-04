package sweep

import (
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func TestRenderNamesEveryReaderAndDuplication(t *testing.T) {
	lines := []PackageLines{{Package: "corpus", Source: 483, Test: 1337}}
	body := Render("test-machine", "2026-09-23", lines)

	for _, want := range []string{"bench/kit/corpus", "bench/suites/context/schemas", "bench/suites/agent/turn", "bench/suites/context/recall", "bench/suites/judge/picker", "bench/suites/rules/rules", "internal/judge/ledger"} {
		if !strings.Contains(body, want) {
			t.Fatalf("report does not name %s", want)
		}
	}
	if !strings.Contains(body, "recommendation:") {
		t.Fatal("report has no recommendation line for a duplication")
	}
	if !strings.Contains(body, "Looks like duplication and is not") {
		t.Fatal("report has no not-duplication section")
	}
	if !strings.Contains(body, "| corpus | 483 | 1337 |") {
		t.Fatal("report does not carry the per-package line count table")
	}
}

func TestCountLinesFindsTheCorpusPackage(t *testing.T) {
	lines, err := CountLines(filepath.Join(sys.SourceRoot(), "bench"))
	if err != nil {
		t.Fatalf("CountLines: %v", err)
	}
	found := false
	for _, l := range lines {
		if l.Package == "kit/corpus" {
			found = true
			if l.Source == 0 {
				t.Fatal("bench/kit/corpus has source files, counted 0")
			}
		}
	}
	if !found {
		t.Fatal("CountLines over bench did not find the corpus package")
	}
}
