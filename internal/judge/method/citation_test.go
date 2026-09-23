package method_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/judge/method"
)

func writeReport(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", full, err)
	}
}

func TestAMeasuredPathThatDoesNotExistFailsCitationCheck(t *testing.T) {
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "x", Line: 5, Measured: "bench/nowhere/report-2026-01-01.md"},
	}}
	errs := method.CheckCitations(table, t.TempDir())
	var missing method.MeasuredPathError
	if len(errs) != 1 || !errors.As(errs[0], &missing) {
		t.Fatalf("errs = %v, want one MeasuredPathError", errs)
	}
	if missing.Path != "bench/nowhere/report-2026-01-01.md" {
		t.Errorf("named path %q", missing.Path)
	}
}

func TestAFigureNotInTheNamedReportFailsCitationCheck(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/x/report-2026-01-01.md", "the arm kept 9 percent of nothing")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "x", Line: 5, Measured: "bench/x/report-2026-01-01.md, 42 percent of gains"},
	}}
	errs := method.CheckCitations(table, root)
	var wrong method.MeasuredFigureError
	if len(errs) != 1 || !errors.As(errs[0], &wrong) {
		t.Fatalf("errs = %v, want one MeasuredFigureError", errs)
	}
	if wrong.Figure != "42%" {
		t.Errorf("named figure %q", wrong.Figure)
	}
}

func TestAFigureThatDoesAppearInTheNamedReportPasses(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/x/report-2026-01-01.md", "the arm kept 42% of gains and cost $0.0012")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "x", Line: 5, Measured: "bench/x/report-2026-01-01.md, 42 percent of gains at $0.0012"},
	}}
	if errs := method.CheckCitations(table, root); len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
}

func TestANewerSiblingReportNotNamedFailsCitationCheck(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/y/report-2026-01-01.md", "the first measurement")
	writeReport(t, root, "bench/y/report-2026-01-02.md", "the second measurement")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "y", Line: 5, Measured: "bench/y/report-2026-01-01.md"},
	}}
	errs := method.CheckCitations(table, root)
	var stale method.UnacknowledgedReportError
	if len(errs) != 1 || !errors.As(errs[0], &stale) {
		t.Fatalf("errs = %v, want one UnacknowledgedReportError", errs)
	}
	if stale.Report != "report-2026-01-02.md" {
		t.Errorf("named report %q", stale.Report)
	}
}

func TestANewerSiblingReportNamedInTheWhyFieldPasses(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/y/report-2026-01-01.md", "the first measurement")
	writeReport(t, root, "bench/y/report-2026-01-02.md", "the second measurement")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "y", Line: 5, Why: "revisited in report-2026-01-02.md", Measured: "bench/y/report-2026-01-01.md"},
	}}
	if errs := method.CheckCitations(table, root); len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
}

func TestAnOlderSiblingReportNeedsNoAcknowledgement(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/z/report-2026-01-01.md", "an earlier, unrelated measurement")
	writeReport(t, root, "bench/z/report-2026-01-05.md", "the cited measurement")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "z", Line: 5, Measured: "bench/z/report-2026-01-05.md"},
	}}
	if errs := method.CheckCitations(table, root); len(errs) != 0 {
		t.Fatalf("errs = %v, want none: an older report is not superseding", errs)
	}
}

func TestAMeasuredLineWithNoReportPathFailsCitationCheck(t *testing.T) {
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "x", Line: 5, Measured: "no report path lives in this line"},
	}}
	errs := method.CheckCitations(table, t.TempDir())
	var bad method.CitationFormatError
	if len(errs) != 1 || !errors.As(errs[0], &bad) {
		t.Fatalf("errs = %v, want one CitationFormatError", errs)
	}
}

func TestAMeasuredNoneRowWhoseBenchHoldsADatedReportFailsCitationCheck(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/spawn/report-2026-09-23.md", "spawn_gate measured for the first time")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "spawn_gate", Line: 5, Measured: "none", Bench: "spawn"},
	}}
	errs := method.CheckCitations(table, root)
	var stale method.StaleAbsenceError
	if len(errs) != 1 || !errors.As(errs[0], &stale) {
		t.Fatalf("errs = %v, want one StaleAbsenceError", errs)
	}
	if stale.Report != "report-2026-09-23.md" {
		t.Errorf("named report %q", stale.Report)
	}
}

func TestAMeasuredNoneRowWhoseBenchHoldsNoDatedReportPasses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bench", "spawn"), 0o755); err != nil {
		t.Fatalf("making bench/spawn: %v", err)
	}
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "spawn_gate", Line: 5, Measured: "none", Bench: "spawn"},
	}}
	if errs := method.CheckCitations(table, root); len(errs) != 0 {
		t.Fatalf("errs = %v, want none: the bench directory holds no dated report", errs)
	}
}

func TestAMeasuredNoneRowNamingNoBenchIsNotChecked(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "bench/spawn/report-2026-09-23.md", "a report exists, and the row names no bench to check it against")
	table := method.Table{File: "constructed.yaml", Choices: []method.Choice{
		{Point: "spawn_gate", Line: 5, Measured: "none"},
	}}
	if errs := method.CheckCitations(table, root); len(errs) != 0 {
		t.Fatalf("errs = %v, want none: no bench field means nothing fuzzy is guessed", errs)
	}
}

func TestTheShippedTableNamesNoStaleCitation(t *testing.T) {
	table := shipped(t)
	errs := method.CheckCitations(table, libraryDir+"/..")
	for _, err := range errs {
		t.Error(err)
	}
}
