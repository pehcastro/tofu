package mutate

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func policyRun(t *testing.T) []Mutant {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "policy-gremlins.txt"))
	if err != nil {
		t.Fatalf("reading the recorded run: %v", err)
	}
	mutants, err := Parse(string(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return mutants
}

func TestTallyAgreesWithTheCountsGremlinsPrintedForItself(t *testing.T) {
	got := Tally(policyRun(t))
	want := Score{Killed: 88, Lived: 16, NotCovered: 5}
	if got != want {
		t.Fatalf("Tally = %+v, want %+v, which is the summary the tool printed at the end of the same run", got, want)
	}
	if math.Abs(got.Efficacy()-84.62) > 0.005 {
		t.Fatalf("Efficacy = %.2f, want 84.62, the figure the tool printed", got.Efficacy())
	}
}

func TestSurvivorsCarryTheFileAndLineOfEveryChangeNobodyNoticed(t *testing.T) {
	survivors := Survivors(policyRun(t))
	if len(survivors) != 16 {
		t.Fatalf("survivors = %d, want 16", len(survivors))
	}
	first := Mutant{Status: Lived, Mutator: "CONDITIONALS_NEGATION", File: "decide.go", Line: 26, Column: 16}
	if survivors[0] != first {
		t.Fatalf("the first survivor is %+v, want %+v", survivors[0], first)
	}
	for _, m := range survivors {
		if m.Status != Lived {
			t.Fatalf("a survivor list holds a %s mutant: %+v", m.Status, m)
		}
	}
}

func TestParseKeepsProseOutAndRefusesAMutantLineItCannotRead(t *testing.T) {
	mutants, err := Parse("Starting...\nGathering coverage... done in 1.5s\n\nMutation testing completed in 1 minute\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(mutants) != 0 {
		t.Fatalf("prose produced %d mutants: %+v", len(mutants), mutants)
	}
	if _, err := Parse("      KILLED CONDITIONALS_NEGATION at decide.go:later:16\n"); err == nil {
		t.Fatal("Parse accepted a mutant line whose line number is not a number")
	}
}
