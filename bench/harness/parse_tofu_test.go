package harness

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
)

func TestParseTofuTurnsTheRecordedLedgerIntoARow(t *testing.T) {
	meta := TofuMeta{
		Arm: ArmTofu, Task: "hono", Version: 1, Run: 1,
		CLIVersion: "n/a", CredentialKind: CredentialKindKey,
	}

	row, gaps, err := ParseTofu(tofuV1Testdata, ledger.Filter{}, meta)
	if err != nil {
		t.Fatalf("ParseTofu: %v", err)
	}

	if row.WallClockMS <= 0 {
		t.Errorf("WallClockMS not positive: %d", row.WallClockMS)
	}
	if row.JudgeDollars <= 0 {
		t.Errorf("JudgeDollars not a positive value: %v", row.JudgeDollars)
	}
	if row.ModelDollars != nil {
		t.Errorf("ParseTofu reads the jev ledger and knows nothing about the model's spend, got %v", *row.ModelDollars)
	}
	if row.Model != "~typesafe/jev-latest" {
		t.Errorf("Model = %q, want the id a real ledger row carries, tilde and all: ~typesafe/jev-latest", row.Model)
	}
	if row.Start.IsZero() || row.End.IsZero() {
		t.Errorf("Start/End should come from the ledger rows' own timestamps, got %v / %v", row.Start, row.End)
	}

	if len(gaps) != 0 {
		t.Errorf("a ledger with matching rows leaves nothing for ParseTofu to report as a gap, got %v", gaps)
	}
}

func TestParseTofuNoMatchIsNamedNotGuessed(t *testing.T) {
	meta := TofuMeta{Arm: ArmTofu, Task: "hono", Version: 1, Run: 1}
	row, gaps, err := ParseTofu(tofuV1Testdata, ledger.Filter{Point: "no_such_point"}, meta)
	if err != nil {
		t.Fatalf("ParseTofu: %v", err)
	}
	if row.JudgeDollars != 0 {
		t.Errorf("no matching rows, JudgeDollars should stay zero, got %v", row.JudgeDollars)
	}
	if !slices.ContainsFunc(gaps, func(gap string) bool { return strings.Contains(gap, "no ledger rows matched") }) {
		t.Errorf("gaps did not name the empty match, got %v", gaps)
	}
}
