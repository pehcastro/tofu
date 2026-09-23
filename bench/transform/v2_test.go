package transform

import (
	"path/filepath"
	"testing"
)

func TestBothArmsOverTheV2MaintenanceSession(t *testing.T) {
	recording := filepath.Join("..", "stopcheck", "corpus", v2MaintenanceSession)
	writes, turns, err := LoadFiles([]string{recording}, filepath.Join("..", "harness", "testdata", "v2-before"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Measure(writes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + Report(rows, FitTokens(writes), turns))

	var preexisting, refused int
	for _, row := range rows {
		if !row.Expressible {
			refused++
		}
		if row.Before != "" {
			preexisting++
		}
	}
	t.Logf("%d writes, %d to a file that already existed, %d refused", len(rows), preexisting, refused)
	if refused > 0 {
		t.Errorf("the typed set still cannot express %d of %d writes", refused, len(rows))
	}
	if preexisting < 5 {
		t.Errorf("v2 exists to record edits to code that already exists and holds %d", preexisting)
	}
}
