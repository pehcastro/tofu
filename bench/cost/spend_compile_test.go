package cost_test

import (
	"os/exec"
	"strings"
	"testing"
)

func buildProbe(t *testing.T, pkg string) (string, error) {
	t.Helper()
	command := exec.Command("go", "build", "-o", t.TempDir(), "./testdata/"+pkg)
	out, err := command.CombinedOutput()
	return string(out), err
}

func TestSummingAnActualSpendWithAListPriceDoesNotCompile(t *testing.T) {
	out, err := buildProbe(t, "spendsum")
	if err == nil {
		t.Fatalf("testdata/spendsum compiled, so ledger.Money and ledger.ListPrice are still addable:\n%s", out)
	}
	if !strings.Contains(out, "mismatched types ledger.Money and ledger.ListPrice") {
		t.Fatalf("testdata/spendsum failed to build for the wrong reason:\n%s", out)
	}
	t.Logf("go build ./bench/cost/testdata/spendsum refused it: %s", strings.TrimSpace(out))
}

func TestKeepingAnActualSpendApartFromAListPriceCompiles(t *testing.T) {
	if out, err := buildProbe(t, "spendapart"); err != nil {
		t.Fatalf("testdata/spendapart must build, the refusal is meant to be about mixing the two units only:\n%s\n%v", out, err)
	}
}
