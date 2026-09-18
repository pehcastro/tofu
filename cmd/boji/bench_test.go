package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	benchapi "boji/bench/api"
	"boji/internal/judge/jev"
)

func TestBenchOfflineSkipsWithoutNetwork(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := benchVerb([]string{"api", "--offline"}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Fatalf("stdout = %q, want it to say the run was skipped", out.String())
	}
}

func TestBenchRejectsAnUnknownTarget(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := benchVerb([]string{"turn"}, out, errOut)
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
}

func TestBenchAPILive(t *testing.T) {
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

	key, err := jev.Key(".env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	call := benchapi.Ask(context.Background(), wire, jev.Request{
		State:     map[string]any{"cwd": "/home/user/project"},
		Questions: []jev.Question{{ID: "q", Kind: jev.QuestionNoul, Instructions: "Does the state name a cwd?", True: "yes", False: "no"}},
	})
	if call.Err != nil {
		t.Fatalf("live request id call: %v", call.Err)
	}
	t.Logf("live request id: %s", call.Response.RequestID)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := benchVerb([]string{"api"}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "bench api:") {
		t.Fatalf("stdout does not look like the report: %q", out.String()[:min(200, len(out.String()))])
	}
}
