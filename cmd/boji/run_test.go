package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunVerbRequiresDirAndExitsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runVerb([]string{"do a thing"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
	if !strings.Contains(errOut.String(), "--dir") {
		t.Fatalf("expected the error to name --dir, got %q", errOut.String())
	}
}

func TestRunVerbDryRunPrintsTheRequestAndMakesNoCall(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	dir := t.TempDir()
	var out, errOut bytes.Buffer

	code := runVerb([]string{"--dir", dir, "--dry-run", "write hello.txt"}, &out, &errOut)

	if code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
	var body map[string]any
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("expected the printed request to be valid JSON: %v", err)
	}
	if body["model"] == "" || body["model"] == nil {
		t.Fatalf("expected the request to carry a model, got %v", body)
	}
	if _, ok := body["messages"]; !ok {
		t.Fatalf("expected the request to carry messages, got %v", body)
	}
	if _, ok := body["tools"]; !ok {
		t.Fatalf("expected the request to carry the tool definitions, got %v", body)
	}
}

func TestRunVerbRequiresATask(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", dir, "--dry-run"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
}

func TestRunVerbRejectsAMissingWorkingDirectory(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", "does-not-exist-anywhere", "--dry-run", "a task"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
}
