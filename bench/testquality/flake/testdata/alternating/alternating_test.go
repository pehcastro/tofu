package alternating

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAlternatesOnAMarkerFile(t *testing.T) {
	dir := os.Getenv("FLAKE_FIXTURE_DIR")
	if dir == "" {
		t.Skip("FLAKE_FIXTURE_DIR is unset")
	}
	marker := filepath.Join(dir, "seen")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("this fixture fails on every run after the first")
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAlwaysPasses(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic broke")
	}
}
