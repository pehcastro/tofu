package sys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheNewDirectoryWinsAndTheOldOneIsReadOnlyWhileItIsAlone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present []string
		want    string
	}{
		{name: "both present", present: []string{StateDirName, LegacyStateDirName}, want: StateDirName},
		{name: "only the old one", present: []string{LegacyStateDirName}, want: LegacyStateDirName},
		{name: "only the new one", present: []string{StateDirName}, want: StateDirName},
		{name: "neither", present: nil, want: StateDirName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			for _, dir := range tc.present {
				if err := os.MkdirAll(filepath.Join(parent, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := StateDir(parent); got != filepath.Join(parent, tc.want) {
				t.Fatalf("StateDir read %s, want %s", got, filepath.Join(parent, tc.want))
			}
		})
	}
}

func TestAFileNamedLikeADataDirectoryIsNotOne(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, StateDirName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, LegacyStateDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := StateDir(parent); got != filepath.Join(parent, LegacyStateDirName) {
		t.Fatalf("StateDir read %s, want the old directory", got)
	}
}
