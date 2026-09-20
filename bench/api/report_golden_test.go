package api_test

import (
	"os"
	"path/filepath"
	"testing"

	"tofu/bench/api"
	"tofu/bench/report"
)

func TestReportRenderingMatchesTheGoldenFile(t *testing.T) {
	result := api.Result{
		Build: "typesafe/jev-1.00-20260101",
		SizeLatencies: []api.SizeLatency{
			{Label: "300", Runs: 3, MedianMS: 300, P95MS: 320, P99MS: 330, MinMS: 290, MaxMS: 340, BilledInput: []int{500, 500, 500}, Build: "typesafe/jev-1.00-20260101"},
		},
		CountLatencies: []api.CountLatency{
			{Count: 1, Runs: 3, MedianMS: 300},
		},
		GateCases: []api.GateCaseResult{
			{Name: "case-1-ls.json", Answers: map[string]float64{"risk": 0, "approval": 0.1, "user_requested": 0.6, "from_untrusted": 0.02}, LatencyMS: 310},
		},
		GateMedianMS:    310,
		GateP95MS:       310,
		GateP99MS:       310,
		GateMinMS:       310,
		GateMaxMS:       310,
		GateSampleCount: 1,
		RerunResults: []api.RerunQuestion{
			{ID: "approval", Values: []float64{0.1, 0.11}, Min: 0.1, Max: 0.11, Spread: 0.01, Straddle: false},
		},
		RerunCase: "case-1-ls.json",
		OptionSweep: []api.OptionSweepPoint{
			{Options: 8, Succeeded: true, Correct: true, LatencyMS: 300, BilledInput: 360, Confidence: 1, Cost: 0.000015},
			{Options: 256, Succeeded: false, ServerError: "too many choices"},
		},
		TotalCost:  0.000200,
		TotalCalls: 4,
	}
	conditions := report.Conditions{
		Machine:        "TESTBOX",
		CredentialKind: "key",
		Wire:           "openrouter",
		Date:           "2026-01-02",
	}
	got := report.Render(result, conditions)

	goldenPath := filepath.Join("testdata", "report.golden.md")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Fatalf("report.Render output does not match %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, string(want))
	}
}
