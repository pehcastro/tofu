package dummy

import (
	"os"
	"testing"
)

func TestEveryCategoryCarriesItsProvenance(t *testing.T) {
	entries, err := os.ReadDir(Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(Path(entry.Name(), "PROVENANCE.md")); err != nil {
			t.Errorf("category %s has no PROVENANCE.md: %v", entry.Name(), err)
		}
	}
}
