package harness

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	fakeArmDirEnv = "TOFU_BENCH_FAKE_ARM_DIR"
	fakeArmLogEnv = "TOFU_BENCH_FAKE_ARM_LOG"
	fakeArmLeaves = "left-behind-by-the-arm.ts"
)

func TestFakeArmRecordsWhatItStartedFromAndLeavesAFileBehind(t *testing.T) {
	dir, log := os.Getenv(fakeArmDirEnv), os.Getenv(fakeArmLogEnv)
	if dir == "" || log == "" {
		t.Skip("this is the fake arm, and it runs only in the subprocess the repeat test spawns")
	}
	line, err := json.Marshal(slices.Sorted(maps.Keys(filesUnder(t, dir))))
	if err != nil {
		t.Fatalf("the fake arm could not record what it found: %v", err)
	}
	handle, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("the fake arm could not open its log: %v", err)
	}
	defer func() { _ = handle.Close() }()
	if _, err := handle.Write(append(line, '\n')); err != nil {
		t.Fatalf("the fake arm could not write its log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fakeArmLeaves), []byte("the arm wrote this and the next repeat must not see it"), 0o600); err != nil {
		t.Fatalf("the fake arm could not write into its tree: %v", err)
	}
}

func TestEveryRepeatStartsFromTheSeedAndCarriesItOnItsRow(t *testing.T) {
	root := repositoryRoot(t)
	armDir := filepath.Join(playground(t), "hono-v1-tofu")
	log := filepath.Join(t.TempDir(), "fake-arm.log")
	t.Setenv(fakeArmDirEnv, armDir)
	t.Setenv(fakeArmLogEnv, log)

	plan, err := BuildPlan(root, ArmTofu, "hono", 1)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	plan.Dir, plan.Repeats = armDir, 2
	plan.Command = []string{os.Args[0], "-test.run=^TestFakeArmRecordsWhatItStartedFromAndLeavesAFileBehind$"}
	plan.Caps = Caps{WallClock: time.Minute}

	repeats, err := RunRepeats(context.Background(), root, plan, func(_ Execution, meta RunMeta) (Row, []string, error) {
		return Row{Arm: meta.Arm, Run: meta.Run, Seed: meta.Seed, Effort: meta.Effort}, nil, nil
	})
	if err != nil {
		t.Fatalf("RunRepeats: %v", err)
	}
	if len(repeats) != 2 {
		t.Fatalf("a plan of two repeats produced %d", len(repeats))
	}

	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read what the fake arm found: %v", err)
	}
	started := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(started) != 2 {
		t.Fatalf("the fake arm ran %d times, want 2: %q", len(started), string(body))
	}
	if started[0] != started[1] {
		t.Fatalf("repeat one started from %s and repeat two from %s, so the second measured the first", started[0], started[1])
	}
	seed, err := SeedOf(root, 1)
	if err != nil {
		t.Fatalf("SeedOf: %v", err)
	}
	for _, repeat := range repeats {
		if repeat.Row.Seed.Digest != seed.Digest {
			t.Errorf("run%d records seed digest %q, want the v1 seed %q", repeat.Row.Run, repeat.Row.Seed.Digest, seed.Digest)
		}
		if repeat.Row.Seed.Path != seed.Path {
			t.Errorf("run%d records seed path %q, want %q", repeat.Row.Run, repeat.Row.Seed.Path, seed.Path)
		}
		if repeat.Row.Effort != plan.Effort {
			t.Errorf("run%d records effort %q, want the plan's %q", repeat.Row.Run, repeat.Row.Effort, plan.Effort)
		}
	}
}
