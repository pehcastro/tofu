package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"tofu/bench/harness"
)

func TestWriteRowRefusesASecondWriteToTheSameDatedPath(t *testing.T) {
	t.Chdir(t.TempDir())
	row := harness.Row{Arm: harness.ArmTofu, Version: 1, Run: 1, Start: time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)}
	path, err := writeRow(row)
	if err != nil {
		t.Fatalf("first writeRow: %v", err)
	}
	_, err = writeRow(row)
	if err == nil {
		t.Fatalf("writeRow(%v) a second time returned nil, wanted a refusal because %s is already on disk", row, path)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the refusal does not name %s: %v", path, err)
	}
	if !strings.Contains(err.Error(), "row") {
		t.Fatalf("the refusal does not carry the noun %q: %v", "row", err)
	}
	t.Logf("bench/cmd/harness.go writeRow refused: %v", err)
}

func TestWriteReportRefusesASecondWriteToTheSameDatedPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("OPENROUTER_KEY=not-a-real-key\n"), 0o600); err != nil {
		t.Fatalf("seeding a fake .env: %v", err)
	}
	produce := func(ctx context.Context, key string) (string, string, error) {
		return "bench/api/report-2026-09-23.md", "the body of a report that must not land twice", nil
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if code := writeReport(out, errOut, "api", produce); code != exitOK {
		t.Fatalf("first writeReport: exit code = %d, stderr %q", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := writeReport(out, errOut, "api", produce); code == exitOK {
		t.Fatalf("writeReport a second time exited %d, wanted a refusal because the report is already on disk", code)
	}
	if !strings.Contains(errOut.String(), "bench/api/report-2026-09-23.md") {
		t.Fatalf("the refusal does not name the path: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "report") {
		t.Fatalf("the refusal does not carry the noun %q: %q", "report", errOut.String())
	}
	t.Logf("bench/cmd/main.go writeReport refused: %s", strings.TrimSpace(errOut.String()))
}
