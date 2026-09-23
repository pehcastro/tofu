package references_test

import (
	"os"
	"strings"
	"testing"
)

const (
	onScreenPage    = "what-reaches-a-screen.md"
	quotePage       = "quote-resolution.md"
	onScreenCitedBy = "also: library/general/references/what-reaches-a-screen.md"
)

func TestTheOnScreenPageSaysWhatReachesAScreenAndWhatScrubDoesNotDo(t *testing.T) {
	body, err := os.ReadFile(onScreenPage)
	if err != nil {
		t.Fatalf("reading %s: %v", onScreenPage, err)
	}
	said := string(body)
	for _, must := range []string{
		"drawn whole",
		"arguments JSON is never drawn",
		"tool names only",
		"keeps every value byte",
		"fixture anonymiser",
		"110 recorded turns",
		"Zero real credentials",
	} {
		if !strings.Contains(said, must) {
			t.Errorf("%s does not say %q, and that is one of the things a person opens it for", onScreenPage, must)
		}
	}
}

func TestTheQuoteReferenceCitesTheOnScreenPage(t *testing.T) {
	body, err := os.ReadFile(quotePage)
	if err != nil {
		t.Fatalf("reading %s: %v", quotePage, err)
	}
	if !strings.Contains(string(body), onScreenCitedBy) {
		t.Fatalf("%s no longer carries %q, so nothing in the library points at %s and nobody opens it", quotePage, onScreenCitedBy, onScreenPage)
	}
}
