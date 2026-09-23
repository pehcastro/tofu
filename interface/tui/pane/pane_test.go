package pane

import (
	"slices"
	"testing"
)

func TestFillKeepsTheHeadOfAListThatDoesNotFitBecauseEveryCallerPutsItsTitleThere(t *testing.T) {
	lines := []string{"title", "", "first row", "second row", "hint"}

	kept := Fill(lines, 3, 5)
	if !slices.Equal(kept, lines[:3]) {
		t.Fatalf("a 5 line list in 3 rows drew %q, want the first three %q: the title lives on line one", kept, lines[:3])
	}
}
