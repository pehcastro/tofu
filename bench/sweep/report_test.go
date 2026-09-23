package sweep

import (
	"strings"
	"testing"
)

func TestRenderNamesEveryReaderAndDuplication(t *testing.T) {
	lines := []PackageLines{{Package: "corpus", Source: 483, Test: 1337}}
	body := Render("test-machine", "2026-09-23", lines)

	for _, want := range []string{"bench/corpus", "bench/schemas", "bench/turn", "bench/recall", "bench/picker", "bench/rules", "internal/judge/ledger"} {
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

func TestCountLinesFindsTheStatPackage(t *testing.T) {
	lines, err := CountLines("..")
	if err != nil {
		t.Fatalf("CountLines: %v", err)
	}
	found := false
	for _, l := range lines {
		if l.Package == "stat" {
			found = true
			if l.Source == 0 {
				t.Fatal("bench/stat has source files, counted 0")
			}
		}
	}
	if !found {
		t.Fatal("CountLines(\"..\") did not find the stat package")
	}
}
