package transform

import (
	"fmt"
	"strings"
	"testing"

	"tofu/internal/konst"
)

func TestTheUnifiedHeaderCountsBothSidesOfAHunkInTheMiddleOfAFile(t *testing.T) {
	var before strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&before, "line %02d\n", i)
	}
	after := strings.Replace(before.String(), "line 06\nline 07\n", "first\nsecond\nthird\n", 1)
	want := "--- notes.txt\n+++ notes.txt\n" +
		"@@ -3,8 +3,9 @@\n" +
		" line 03\n line 04\n line 05\n" +
		"-line 06\n-line 07\n" +
		"+first\n+second\n+third\n" +
		" line 08\n line 09\n line 10\n"
	if got := Unified("notes.txt", before.String(), after); got != want {
		t.Fatalf("the diff was\n%s\nwanted\n%s", got, want)
	}
}

func TestOversizedForDiffTripsExactlyOneCellPastTheBound(t *testing.T) {
	if oversizedForDiff(1, konst.DiffTableMaxCells) {
		t.Fatal("a table sized exactly at the bound was refused")
	}
	if !oversizedForDiff(1, konst.DiffTableMaxCells+1) {
		t.Fatal("a table one cell past the bound was accepted")
	}
}

func TestAFilePairPastTheDiffBoundFallsBackToOneHunkInsteadOfBuildingTheTable(t *testing.T) {
	lines := 3163
	if !oversizedForDiff(lines, lines) {
		t.Fatalf("%d lines on each side does not exceed the diff bound, fix the test", lines)
	}
	before := strings.Repeat("a\n", lines)
	after := strings.Repeat("b\n", lines)
	hunks := Hunks(before, after)
	if len(hunks) != 1 {
		t.Fatalf("got %d hunks past the diff bound, wanted the whole file folded into one", len(hunks))
	}
	if len(hunks[0].Removed) != lines || len(hunks[0].Added) != lines {
		t.Fatalf("the fallback hunk held %d removed and %d added, wanted %d of each",
			len(hunks[0].Removed), len(hunks[0].Added), lines)
	}
}

func TestTheFirstAndTheLastLineOfAFileCanBeRemoved(t *testing.T) {
	const head = "--- f.txt\n+++ f.txt\n@@ -1,2 +1,1 @@\n"
	if got := Unified("f.txt", "a\nb\n", "b\n"); got != head+"-a\n b\n" {
		t.Errorf("removing the first line gave\n%s", got)
	}
	if got := Unified("f.txt", "a\nb\n", "a\n"); got != head+" a\n-b\n" {
		t.Errorf("removing the last line gave\n%s", got)
	}
}
