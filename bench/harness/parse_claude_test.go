package harness

import (
	"os"
	"strings"
	"testing"
)

func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return data
}

func TestParseClaudeTurnsTheRecordedTranscriptIntoARow(t *testing.T) {
	data := loadTestdata(t, "claude-p1.json")
	meta := ClaudeMeta{
		Arm: ArmClaude, Task: "hono", Version: 1, Run: 1,
		CLIVersion: "2.1.277", CredentialKind: CredentialKindKey,
	}

	row, gaps, err := ParseClaude(data, meta)
	if err != nil {
		t.Fatalf("ParseClaude: %v", err)
	}

	if row.WallClockMS <= 0 {
		t.Errorf("WallClockMS not positive: %d", row.WallClockMS)
	}
	if row.BilledInput <= 0 {
		t.Errorf("BilledInput not positive: %d", row.BilledInput)
	}
	if row.BilledOutput <= 0 {
		t.Errorf("BilledOutput not positive: %d", row.BilledOutput)
	}
	if row.Turns <= 0 {
		t.Errorf("Turns not positive: %d", row.Turns)
	}
	if row.ModelDollars == nil || *row.ModelDollars <= 0 {
		t.Errorf("ModelDollars not a positive value: %v", row.ModelDollars)
	}
	if row.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model = %q, want claude-haiku-4-5-20251001", row.Model)
	}
	if row.EndReason != EndReasonDone {
		t.Errorf("EndReason = %q, want done", row.EndReason)
	}
	if row.ToolCalls != (ToolCalls{}) {
		t.Errorf("ToolCalls should be genuinely zero under --restricted, got %+v", row.ToolCalls)
	}

	wantGap := "start/end timestamps"
	found := false
	for _, g := range gaps {
		if strings.Contains(g, wantGap) {
			found = true
		}
	}
	if !found {
		t.Errorf("gaps did not name %q, got %v", wantGap, gaps)
	}
}

func TestParseClaudeSubscriptionDollarsAreNullEvenWhenTheTranscriptReportsACost(t *testing.T) {
	data := loadTestdata(t, "claude-p1.json")
	meta := ClaudeMeta{
		Arm: ArmClaude, Task: "hono", Version: 1, Run: 1,
		CLIVersion: "2.1.277", CredentialKind: CredentialKindSubscription,
	}

	row, _, err := ParseClaude(data, meta)
	if err != nil {
		t.Fatalf("ParseClaude: %v", err)
	}
	if row.ModelDollars != nil {
		t.Errorf("subscription row should have nil ModelDollars, got %v", *row.ModelDollars)
	}
}
