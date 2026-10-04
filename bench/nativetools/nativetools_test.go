package nativetools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
)

const big = "../../.playground/big"

func TestNativeTools(t *testing.T) {
	if _, err := os.Stat(filepath.Join(big, "runs")); err != nil {
		t.Skipf("skipped: %s/runs is not on this machine, so there is no recorded call to replay", big)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("skipped: no sh on PATH, so the native arms cannot run")
	}
	corpus, err := Load(big)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Edits) == 0 || len(corpus.Reads) == 0 || len(corpus.Lists) == 0 {
		t.Fatalf("the recordings gave %d edits, %d reads and %d listings", len(corpus.Edits), len(corpus.Reads), len(corpus.Lists))
	}
	scratch := t.TempDir()
	for dir := scratch; filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err == nil {
			t.Fatalf("%s holds a tsconfig.json, so every edit would start a real typecheck", dir)
		}
	}
	edits, err := RunEdits(corpus, filepath.Join(scratch, "edits"))
	if err != nil {
		t.Fatal(err)
	}
	reads, err := RunReads(corpus, filepath.Join(scratch, "reads"))
	if err != nil {
		t.Fatal(err)
	}
	seedState := version(corpus.Seed, "echo HEAD $(git rev-parse --short HEAD), $(git status --porcelain | wc -l) changed paths")
	lists, err := RunLists(corpus)
	if err != nil {
		t.Fatal(err)
	}
	machine, _ := os.Hostname()
	report := Render(Conditions{
		Machine: machine, Date: time.Now().Format("2006-01-02"), OS: runtime.GOOS + "/" + runtime.GOARCH,
		Shell: version(scratch, "command -v sh"), Sed: version(scratch, "sed --version | head -n 1"),
		Find: version(scratch, "find --version | head -n 1"), SeedState: seedState, Cap: konst.TurnResultBytesCap,
	}, corpus, edits, reads, lists)
	t.Log(report)
	if path := os.Getenv("NATIVETOOLS_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(report), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func version(dir, script string) string {
	out, _, err := shell(dir, script)
	if err != nil {
		return err.Error()
	}
	return strings.Join(strings.Fields(out), " ")
}
