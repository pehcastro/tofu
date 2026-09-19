package rule

import (
	"path/filepath"
	"testing"
)

func TestLoadRejectsAnUnknownModeAsAnError(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "unknown_mode.yaml"))
	if err == nil {
		t.Fatal("Load returned no error for an unknown mode")
	}
}

func TestLoadDefaultsToShadowWhenModeIsMissing(t *testing.T) {
	r, err := Load(filepath.Join("testdata", "no_mode.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.ModeDeclared {
		t.Fatal("ModeDeclared = true, want false, no mode key was present")
	}
	if r.Mode != ModeShadow {
		t.Fatalf("Mode = %s, want %s", r.Mode, ModeShadow)
	}
}

func TestLoadDirLoadsEveryShippedRule(t *testing.T) {
	rules, err := LoadDir(filepath.Join("..", "..", "catalog", "rules"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("rules = %d, want 4: %+v", len(rules), rules)
	}
	for _, r := range rules {
		if r.Mode != ModeShadow {
			t.Fatalf("rule %q ships in mode %s, want %s, this ticket promotes nothing", r.ID, r.Mode, ModeShadow)
		}
	}
}
