package settings

import (
	"path/filepath"
	"testing"
)

func TestSearchFiltersByLabelAndReportsAMatchCount(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, FileName), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	matches := store.Search("decision")
	if len(matches) != 1 || matches[0].Spec.Key != DecisionCap {
		t.Fatalf("Search(decision) = %v, want only decisionCap", matches)
	}
	if matches := store.Search(""); len(matches) != len(Default()) {
		t.Fatalf("Search(\"\") = %d matches, want every row", len(matches))
	}
	if matches := store.Search("zzzzz"); len(matches) != 0 {
		t.Fatalf("Search(zzzzz) = %v, want no matches", matches)
	}
}
