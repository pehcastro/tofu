package whatif

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
)

func TestTryStopsTheCopy(t *testing.T) {
	project := t.TempDir()
	body := []byte(strings.Repeat("x", 16<<10))
	for i := range 8000 {
		path := filepath.Join(project, fmt.Sprintf("d%d", i%80), fmt.Sprintf("f%d.txt", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := contract.NewRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	stopAt := 200 * time.Millisecond
	for _, c := range []struct {
		name   string
		limit  time.Duration
		cancel bool
		want   Outcome
	}{
		{"cancelled mid copy", time.Minute, true, NotRun},
		{"copy past its deadline", stopAt, false, TimedOut},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if c.cancel {
				time.AfterFunc(stopAt, cancel)
			}
			started := time.Now()
			answer, err := Try(ctx, root, map[string]string{"d0/f0.txt": "edited"}, Check{Kind: Command, Command: []string{"git", "--version"}}, Limits{Check: c.limit, StaleOverlay: time.Hour})
			took := time.Since(started)
			if err != nil || answer.Outcome != c.want || took > stopAt+2*time.Second {
				t.Fatalf("got %v, %v after %v, want %v within %v", answer.Outcome, err, took, c.want, stopAt+2*time.Second)
			}
			scratchRoot, err := scratch.ScratchRootAt(project)
			if err != nil {
				t.Fatal(err)
			}
			if left, _ := os.ReadDir(filepath.Join(scratchRoot, "overlays")); len(left) != 0 {
				t.Fatalf("%d overlay folders left behind", len(left))
			}
		})
	}
}
