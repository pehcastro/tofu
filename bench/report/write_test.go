package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRefusesAFileAlreadyOnDisk(t *testing.T) {
	for _, tt := range []struct {
		name string
		noun string
	}{
		{"report-2026-09-19.md", "report"},
		{"claude-v1-run1.json", "row"},
	} {
		dir := t.TempDir()
		existing := filepath.Join(dir, tt.name)
		if err := os.WriteFile(existing, []byte("already here"), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", existing, err)
		}
		err := Write(existing, []byte("a run that must not land here"), 0o644, tt.noun)
		if err == nil {
			t.Fatalf("Write(%s, ...) returned nil, wanted a refusal because the file is already on disk", existing)
		}
		if !strings.Contains(err.Error(), existing) {
			t.Fatalf("the refusal does not name %s: %v", existing, err)
		}
		if !strings.Contains(err.Error(), tt.noun) {
			t.Fatalf("the refusal does not carry the noun %q: %v", tt.noun, err)
		}
		t.Logf("refused: %v", err)
	}
}
