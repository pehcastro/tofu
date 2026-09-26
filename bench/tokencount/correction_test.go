package tokencount

import (
	"strings"
	"testing"
)

func TestFitFactorsFitsOnlyAccountableSamplesPerShape(t *testing.T) {
	samples := []Sample{
		{Shape: ShapeProse, Bytes: 40, Actual: 10, Accountable: true},
		{Shape: ShapeProse, Bytes: 20, Actual: 5, Accountable: true},
		{Shape: ShapeToolCallArgs, Bytes: 9, Actual: 3, Accountable: true},
		{Shape: ShapeToolCallArgs, Bytes: 2, Actual: 30, Accountable: false},
	}
	factors := FitFactors(samples)
	if len(factors) != 2 {
		t.Fatalf("FitFactors returned %d shapes, want 2", len(factors))
	}
	byShape := map[Shape]Factor{}
	for _, f := range factors {
		byShape[f.Shape] = f
	}
	prose := byShape[ShapeProse]
	if prose.N != 2 {
		t.Fatalf("prose N = %d, want 2", prose.N)
	}
	if prose.BytesPerToken != 4 {
		t.Fatalf("prose BytesPerToken = %v, want 4 (median of 40/10=4 and 20/5=4)", prose.BytesPerToken)
	}
	tool := byShape[ShapeToolCallArgs]
	if tool.N != 1 {
		t.Fatalf("tool N = %d, want 1: the unaccountable sample must not be fitted on", tool.N)
	}
	if tool.BytesPerToken != 3 {
		t.Fatalf("tool BytesPerToken = %v, want 3 (9/3)", tool.BytesPerToken)
	}
}

func TestFitFactorsZeroesResidualWhenTheRatioIsExact(t *testing.T) {
	samples := []Sample{
		{Shape: ShapeProse, Bytes: 40, Actual: 10, Accountable: true},
		{Shape: ShapeProse, Bytes: 20, Actual: 5, Accountable: true},
	}
	factors := FitFactors(samples)
	if len(factors) != 1 {
		t.Fatalf("got %d factors, want 1", len(factors))
	}
	if factors[0].After.MedianPct != 0 {
		t.Fatalf("After.MedianPct = %v, want 0 when the fitted ratio reproduces every sample exactly", factors[0].After.MedianPct)
	}
}

func TestCorrectedAccountableLeavesUnaccountableSamplesOut(t *testing.T) {
	samples := []Sample{
		{Shape: ShapeProse, Bytes: 40, Actual: 10, Accountable: true},
		{Shape: ShapeProse, Bytes: 2, Actual: 30, Accountable: false},
	}
	factors := FitFactors(samples)
	corrected := CorrectedAccountable(samples, factors)
	if len(corrected) != 1 {
		t.Fatalf("CorrectedAccountable returned %d samples, want 1", len(corrected))
	}
	if !corrected[0].Accountable {
		t.Fatalf("corrected sample lost its Accountable flag")
	}
}

func TestErrorPctHandlesZeroActual(t *testing.T) {
	if got := errorPct(5, 0); got != 0 {
		t.Fatalf("errorPct(5, 0) = %v, want 0", got)
	}
}

func TestRenderCorrectionCoversEveryAcceptanceLine(t *testing.T) {
	result, err := Run(sessionsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rendered := RenderCorrection(ReportInput{Date: "2026-09-24", Machine: "TESTHOST", TestCount: 15, Result: result})
	for _, want := range []string{
		"bytes/token", "Residual error after correction", "What the factor cannot fix",
		"Which figures move if the constant changes", "bench/schemas/report-2026-09-23.md",
		"bench/prefix/report-2026-09-23.md", "bench/tokens/report-2026-09-21.md",
		"bench/linenumbers/report-2026-09-23.md", "bench/tokencount/report-2026-09-24.md",
		"konst.SearchBytesPerToken is unchanged",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("report carries no %q", want)
		}
	}
	t.Log("\n" + rendered)
}
