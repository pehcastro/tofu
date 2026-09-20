package mutate

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCommandPutsThePackagePatternLastAndRunsInTheDirectoryItWasGiven(t *testing.T) {
	cmd := Gremlins.Command(context.Background(), "somewhere-else", "./internal/judge/policy/")
	if cmd.Dir != "somewhere-else" {
		t.Fatalf("the command runs in %q, want the directory it was given", cmd.Dir)
	}
	argv := strings.Join(cmd.Args[1:], " ")
	if argv != "unleash --timeout-coefficient 30 --workers 4 ./internal/judge/policy/" {
		t.Fatalf("argv = %q", argv)
	}
}

func TestRunReadsMutantsFromWhateverTheToolPrintedAndFailsWhenThereIsNoTool(t *testing.T) {
	quiet := Tool{Name: "quiet", Binary: "go", Args: []string{"env"}}
	outcome, err := quiet.Run(context.Background(), ".", "GOVERSION")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(outcome.Mutants) != 0 {
		t.Fatalf("output with no mutant lines produced %d mutants", len(outcome.Mutants))
	}
	if outcome.Tool != "quiet" || outcome.Package != "GOVERSION" {
		t.Fatalf("the outcome does not say what was run: %+v", outcome)
	}

	missing := Tool{Name: "missing", Binary: "no-such-mutation-tool", Args: nil}
	if _, err := missing.Run(context.Background(), ".", "./..."); err == nil {
		t.Fatal("Run returned no error for a tool that is not installed")
	}
}

func TestRenderNamesEverySurvivorAndWhatWasChangedInIt(t *testing.T) {
	outcome := Outcome{Tool: "gremlins", Package: "./internal/judge/policy/", Elapsed: 63 * time.Second, Mutants: policyRun(t)}
	rendered := Render(outcome)
	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	if len(lines) != 17 {
		t.Fatalf("rendered %d lines, want a score line and 16 survivors:\n%s", len(lines), rendered)
	}
	if !strings.Contains(lines[0], "84.62") || !strings.Contains(lines[0], "1m3s") {
		t.Fatalf("the score line does not carry the efficacy and the wall clock: %q", lines[0])
	}
	if !strings.Contains(lines[1], "decide.go:26:16") || !strings.Contains(lines[1], "negated") {
		t.Fatalf("the first survivor line does not say where it is and what was changed: %q", lines[1])
	}
}

func TestChangeFallsBackToTheMutatorNameItWasNotTaught(t *testing.T) {
	if got := Change("CONDITIONALS_BOUNDARY"); !strings.Contains(got, ">=") {
		t.Fatalf("Change(CONDITIONALS_BOUNDARY) = %q, want the boundary move spelled out", got)
	}
	if got := Change("SOMETHING_NEW"); got != "SOMETHING_NEW" {
		t.Fatalf("Change of an unknown mutator = %q, want the name the tool printed", got)
	}
}
