package fire

import (
	"testing"

	"tofu/bench/kit/corpus/recorded"
	"tofu/internal/sys"
)

var repoRoot = sys.SourceRoot()

func TestReadDirReadsTheRealLog(t *testing.T) {
	fires, unreadable, err := ReadDir(recorded.State(t, "log"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if unreadable != 0 {
		t.Fatalf("unreadable = %d, want 0", unreadable)
	}
	if len(fires) != 239 {
		t.Fatalf("fires = %d, want 239; the log moved and this report needs a rerun", len(fires))
	}
}

func TestReadDirOnAnEmptyDirectoryFindsNothing(t *testing.T) {
	fires, unreadable, err := ReadDir(t.TempDir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(fires) != 0 {
		t.Fatalf("fires = %d, want 0", len(fires))
	}
	if unreadable != 0 {
		t.Fatalf("unreadable = %d, want 0", unreadable)
	}
}
