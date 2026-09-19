package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineSkipsWithoutNetwork(t *testing.T) {
	for _, target := range []string{"api", "cost", "wording", "turn"} {
		out := &bytes.Buffer{}
		errOut := &bytes.Buffer{}
		if code := run([]string{target, "--offline"}, out, errOut); code != exitOK {
			t.Fatalf("%s: exit code = %d, want %d, stderr %q", target, code, exitOK, errOut.String())
		}
		if !strings.Contains(out.String(), "skipped") {
			t.Fatalf("%s: stdout = %q, want it to say the run was skipped", target, out.String())
		}
	}
}

func TestUnknownTargetExitsUsageAndNamesIt(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := run([]string{"cst", "--offline"}, out, errOut); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), `"cst"`) {
		t.Fatalf("stderr = %q, want it to name the target", errOut.String())
	}
}

func TestBojiUnderTestTakesTheNamedBinaryAndBuildsNothing(t *testing.T) {
	named := filepath.Join(t.TempDir(), "boji-that-does-not-exist")
	path, cleanup, err := bojiUnderTest(named)
	if err != nil {
		t.Fatalf("bojiUnderTest: %v", err)
	}
	defer cleanup()
	if path != named {
		t.Fatalf("path = %q, want the named binary %q", path, named)
	}
}

func TestHarnessOfflineNeedsNoBojiBinary(t *testing.T) {
	transcript := t.TempDir()
	session := filepath.Join(transcript, "session.json")
	body := `{"id":"turn-1","schema":1,"at":"2026-09-18T10:00:00Z","task":"hono","model":"m","spend":"api_key","outcome":"stopped","total_cost_usd":0.5,"wall_clock_ms":10}`
	if err := os.WriteFile(session, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the transcript: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatalf("chdir to the repository root: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := benchHarness(out, errOut, []string{"--offline", "--transcript", transcript}); code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "ROW boji hono v1 run1") {
		t.Fatalf("stdout does not carry the row: %q", out.String())
	}
	if strings.Contains(out.String(), "running:") {
		t.Fatalf("the offline arm executed something: %q", out.String())
	}
}

func TestAPILive(t *testing.T) {
	if os.Getenv("BOJI_LIVE") != "1" {
		t.Skip("set BOJI_LIVE=1 to call the real route")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatalf("chdir to the repository root: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := run([]string{"api"}, out, errOut); code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "bench api:") {
		t.Fatalf("stdout does not look like the report: %q", out.String()[:min(200, len(out.String()))])
	}
}
