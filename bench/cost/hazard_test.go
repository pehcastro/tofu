package cost

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"tofu/bench/report"
	"tofu/internal/judge/jev/wire/typesafe"
)

func hazardReport(t *testing.T, rows []hazardRow, answersPath string) string {
	t.Helper()
	arms, err := jevArms(repoRoot)
	if err != nil {
		t.Fatalf("the jev arms: %v", err)
	}
	heldOut, err := heldOutRecords()
	if err != nil {
		t.Fatalf("the held-out half: %v", err)
	}
	scores, err := scoreHazards(rows, arms, heldOut)
	if err != nil {
		t.Fatalf("scoring %s: %v", answersPath, err)
	}
	return renderHazards(scores, rows[0], filepath.ToSlash(answersPath))
}

func TestLiveHazardArmsOverTheHeldOutHalf(t *testing.T) {
	key := liveKey(t, "TOFU_LIVE_HAZARDS")
	arms, err := jevArms(repoRoot)
	if err != nil {
		t.Fatalf("the jev arms: %v", err)
	}
	heldOut, err := heldOutRecords()
	if err != nil {
		t.Fatalf("the held-out half: %v", err)
	}
	probe := len(heldOut)
	if raw := os.Getenv("TOFU_LIVE_HAZARD_CASES"); raw != "" {
		if probe, err = strconv.Atoi(raw); err != nil {
			t.Fatalf("TOFU_LIVE_HAZARD_CASES: %v", err)
		}
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("no hostname for the conditions: %v", err)
	}
	day := time.Now().Format("2006-01-02")
	rows, runErr := runHazardArms(context.Background(), key, arms, heldOut[:probe], report.Conditions{
		Machine: host, CredentialKind: "key", Wire: typesafe.Name, Date: day,
	})
	spent, tokens := 0.0, 0
	for _, row := range rows {
		spent, tokens = spent+row.charge().USD, tokens+row.InputTokens
		t.Logf("%s %s: %s against %s, %d input tokens, %.0f ms, %d attempts, build %s", row.Case, row.Arm, row.Verdict, row.Label, row.InputTokens, row.LatencyMS, row.Attempts, row.Build)
	}
	t.Logf("%d calls, %d input tokens, $%.6f", len(rows), tokens, spent)
	if runErr != nil {
		t.Fatalf("the run stopped: %v", runErr)
	}
	if probe < len(heldOut) {
		return
	}
	answersPath, reportPath := hazardPaths(day)
	if err := writeHazardRows(answersPath, rows); err != nil {
		t.Fatalf("%v", err)
	}
	if err := report.Write(reportPath, []byte(hazardReport(t, rows, answersPath)), 0o644, "report"); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestHazardReportRerendersFromItsAnswers(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("answers", "hazard-*.jsonl"))
	if err != nil || len(paths) == 0 {
		t.Skipf("no recorded hazard run to rerender: %v", err)
	}
	for _, answersPath := range paths {
		rows, err := readHazardRows(answersPath)
		if err != nil {
			t.Fatalf("%v", err)
		}
		_, reportPath := hazardPaths(rows[0].Date)
		recorded, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatalf("%v", err)
		}
		body := hazardReport(t, rows, answersPath)
		t.Log("\n" + body)
		if body != string(recorded) {
			t.Errorf("%s rerenders differently from %s", answersPath, reportPath)
		}
	}
}
