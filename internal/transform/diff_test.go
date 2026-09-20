package transform

import (
	"fmt"
	"strings"
	"testing"
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

func TestTheFirstAndTheLastLineOfAFileCanBeRemoved(t *testing.T) {
	const head = "--- f.txt\n+++ f.txt\n@@ -1,2 +1,1 @@\n"
	if got := Unified("f.txt", "a\nb\n", "b\n"); got != head+"-a\n b\n" {
		t.Errorf("removing the first line gave\n%s", got)
	}
	if got := Unified("f.txt", "a\nb\n", "a\n"); got != head+" a\n-b\n" {
		t.Errorf("removing the last line gave\n%s", got)
	}
}
