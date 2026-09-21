package settings

import (
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/rule"
)

func TestReloadReReadsRulesFromDiskAndNamesWhatItCannotReload(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("one.yaml", "id: one\ndomain: general\nkind: structural\nchecker: alwaysPass\n")

	result, err := Reload(nil, dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if result.Rules != 1 {
		t.Fatalf("Rules = %d, want 1 before the change on disk", result.Rules)
	}

	write("two.yaml", "id: two\ndomain: general\nkind: structural\nchecker: alwaysPass\n")
	result, err = Reload(nil, dir)
	if err != nil {
		t.Fatalf("Reload after the change: %v", err)
	}
	if result.Rules != 2 {
		t.Fatalf("Rules after adding two.yaml = %d, want 2", result.Rules)
	}
	if len(result.Skipped) == 0 {
		t.Fatal("Reload must say what it cannot reload")
	}

	if _, err := Reload([]rule.Rule{}, filepath.Join(dir, "does-not-exist")); err != nil {
		t.Fatalf("Reload with a missing project rule dir must not error: %v", err)
	}
}
