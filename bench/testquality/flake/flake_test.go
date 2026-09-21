package flake

import (
	"errors"
	"slices"
	"testing"
)

const fixturePackage = "./testdata/alternating"

func TestMeasureSeparatesTheTestThatDisagreesFromTheOneThatDoesNot(t *testing.T) {
	t.Setenv("FLAKE_FIXTURE_DIR", t.TempDir())
	report, err := Measure(fixturePackage, 3)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if report.Runs != 3 {
		t.Fatalf("Runs = %d, want 3", report.Runs)
	}
	disagreeing := report.Disagreeing()
	if len(disagreeing) != 1 {
		t.Fatalf("got %d disagreeing tests, want 1: %v", len(disagreeing), report.Tests)
	}
	if disagreeing[0].Test != "TestAlternatesOnAMarkerFile" {
		t.Fatalf("disagreeing test = %q, want TestAlternatesOnAMarkerFile", disagreeing[0].Test)
	}
	want := []Outcome{Passed, Failed, Failed}
	if !slices.Equal(disagreeing[0].Outcomes, want) {
		t.Fatalf("outcomes = %v, want %v", disagreeing[0].Outcomes, want)
	}
}

func TestMeasureCountsASkipAsAResultRatherThanAnAbsence(t *testing.T) {
	t.Setenv("FLAKE_FIXTURE_DIR", "")
	report, err := Measure(fixturePackage, 2)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	skipped := report.AlwaysSkipped()
	if len(skipped) != 1 {
		t.Fatalf("got %d always-skipped tests, want 1: %v", len(skipped), report.Tests)
	}
	if skipped[0].Test != "TestAlternatesOnAMarkerFile" {
		t.Fatalf("skipped test = %q, want TestAlternatesOnAMarkerFile", skipped[0].Test)
	}
	if len(report.Disagreeing()) != 0 {
		t.Fatalf("a test skipped in every run must not count as disagreeing: %v", report.Disagreeing())
	}
}

func TestMeasureRefusesFewerThanTwoRuns(t *testing.T) {
	_, err := Measure(fixturePackage, 1)
	if !errors.Is(err, ErrNoRuns) {
		t.Fatalf("err = %v, want ErrNoRuns", err)
	}
}

func TestCollateTreatsATestMissingFromOneRunAsADisagreement(t *testing.T) {
	report := Collate("p", []map[string]Outcome{
		{"TestA": Passed, "TestB": Passed},
		{"TestA": Passed},
	})
	disagreeing := report.Disagreeing()
	if len(disagreeing) != 1 {
		t.Fatalf("got %d disagreeing tests, want 1: %v", len(disagreeing), report.Tests)
	}
	if disagreeing[0].Test != "TestB" {
		t.Fatalf("disagreeing test = %q, want TestB", disagreeing[0].Test)
	}
	if disagreeing[0].Outcomes[1] != Absent {
		t.Fatalf("second outcome = %q, want absent", disagreeing[0].Outcomes[1])
	}
}
