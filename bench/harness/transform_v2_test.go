package harness

import (
	"os"
	"path/filepath"
	"testing"

	"tofu/bench/transform"
)

func TestTransformArmsOverTheV2Session(t *testing.T) {
	writes, turns, err := transform.LoadFiles([]string{v2SessionRecording}, v2BeforeTestdata)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) == 0 {
		t.Fatalf("the v2 session holds no write call, so there is nothing for either arm to spend on")
	}

	rows, err := transform.Measure(writes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fit := transform.FitTokens(writes)
	t.Log("\n" + transform.Report(rows, fit, turns))

	var preexisting, refusedOnPreexisting int
	var writeOut, writeIn, fallbackOut, fallbackIn int
	for _, row := range rows {
		writeOut, writeIn = writeOut+fit.Tokens(row.WriteOut), writeIn+fit.Tokens(row.WriteIn)
		if row.Expressible {
			fallbackOut, fallbackIn = fallbackOut+fit.Tokens(row.TypedOut), fallbackIn+fit.Tokens(row.TypedIn)
		} else {
			fallbackOut, fallbackIn = fallbackOut+fit.Tokens(row.WriteOut), fallbackIn+fit.Tokens(row.WriteIn)
		}
		if _, err := os.Stat(filepath.Join(v2BeforeTestdata, filepath.FromSlash(row.Path))); err != nil {
			continue
		}
		preexisting++
		if !row.Expressible {
			refusedOnPreexisting++
		}
	}

	t.Logf("%d writes, %d of them to a file that already existed before the run, and the typed set refused %d of those %d",
		len(rows), preexisting, refusedOnPreexisting, preexisting)
	t.Logf("a refusal costs a full write, so the typed arm really spends %d out and %d in against the write arm's %d and %d",
		fallbackOut, fallbackIn, writeOut, writeIn)

	if preexisting < 5 {
		t.Errorf("v2 exists to record edits to code that already exists and it produced %d of them, "+
			"against v1's three: the task is wrong and nothing should be concluded from it", preexisting)
	}
}
