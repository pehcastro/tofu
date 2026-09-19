package harness

import (
	"strings"
	"testing"
	"time"
)

func TestParseCodexTurnsTheRecordedTranscriptIntoARow(t *testing.T) {
	data := loadTestdata(t, "codex-1.jsonl")
	start := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	meta := CodexMeta{
		Arm: ArmCodex, Task: "hono", Version: 1, Run: 1,
		CLIVersion: "codex-cli 0.153.4", CredentialKind: CredentialKindSubscription,
		Start: start, End: start.Add(2 * time.Second),
	}

	row, gaps, err := ParseCodex(data, meta)
	if err != nil {
		t.Fatalf("ParseCodex: %v", err)
	}

	if row.Turns <= 0 {
		t.Errorf("Turns not positive: %d", row.Turns)
	}
	if row.WallClockMS <= 0 {
		t.Errorf("WallClockMS not positive: %d", row.WallClockMS)
	}
	if row.EndReason != EndReasonCrash {
		t.Errorf("EndReason = %q, want crash, this transcript is a real usage-limit error", row.EndReason)
	}
	if row.ModelDollars != nil {
		t.Errorf("ModelDollars should be nil, this transcript never reports a cost, got %v", *row.ModelDollars)
	}
	if row.BilledInput != 0 || row.BilledOutput != 0 {
		t.Errorf("billed tokens should be zero, no turn.completed event exists, got in=%d out=%d", row.BilledInput, row.BilledOutput)
	}

	wantGaps := []string{"billed input/output tokens", "model:", "dollars:", "end reason:"}
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
