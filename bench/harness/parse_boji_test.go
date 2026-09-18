package harness

import (
	"strings"
	"testing"

	"boji/internal/judge/ledger"
)

func TestParseBojiTurnsTheRecordedLedgerIntoARow(t *testing.T) {
	meta := BojiMeta{
		Arm: ArmBoji, Task: "hono", Version: 1, Run: 1,
		CLIVersion: "n/a", CredentialKind: CredentialKindKey,
	}

	row, gaps, err := ParseBoji("testdata/boji-1", ledger.Filter{}, meta)
	if err != nil {
		t.Fatalf("ParseBoji: %v", err)
	}

	if row.WallClockMS <= 0 {
		t.Errorf("WallClockMS not positive: %d", row.WallClockMS)
	}
	if row.Dollars == nil || *row.Dollars <= 0 {
		t.Errorf("Dollars not a positive value: %v", row.Dollars)
	}
	if row.Model != "typesafe/jev-latest" {
		t.Errorf("Model = %q, want typesafe/jev-latest", row.Model)
	}
	if row.Start.IsZero() || row.End.IsZero() {
		t.Errorf("Start/End should come from the ledger rows' own timestamps, got %v / %v", row.Start, row.End)
	}

	wantGaps := []string{"turns:", "tool calls:", "billed input/output tokens:"}
	for _, want := range wantGaps {
		found := false
		for _, g := range gaps {
			if strings.Contains(g, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("gaps did not name %q, got %v", want, gaps)
		}
	}
}

func TestParseBojiNoMatchIsNamedNotGuessed(t *testing.T) {
	meta := BojiMeta{Arm: ArmBoji, Task: "hono", Version: 1, Run: 1}
	row, gaps, err := ParseBoji("testdata/boji-1", ledger.Filter{Point: "no_such_point"}, meta)
	if err != nil {
		t.Fatalf("ParseBoji: %v", err)
	}
	if row.Dollars != nil {
		t.Errorf("no matching rows, Dollars should stay nil, got %v", *row.Dollars)
	}
	found := false
	for _, g := range gaps {
		if strings.Contains(g, "no ledger rows matched") {
			found = true
		}
	}
	if !found {
		t.Errorf("gaps did not name the empty match, got %v", gaps)
	}
}
