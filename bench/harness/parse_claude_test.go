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
	for _, wantGap := range []string{"start/end timestamps", "tool calls", "billed input tokens"} {
		if !gapsName(gaps, wantGap) {
			t.Errorf("gaps did not name %q, got %v", wantGap, gaps)
		}
	}
}

func gapsName(gaps []string, want string) bool {
	for _, g := range gaps {
		if strings.Contains(g, want) {
			return true
		}
	}
	return false
}

func TestParseClaudeOverTheFirstRealRunOfAFullTask(t *testing.T) {
	data, err := os.ReadFile(liveClaudeTranscript)
	if err != nil {
		t.Skipf("no live claude transcript yet: %v. Run TestClaudeArmRunsLiveAndProducesARow, and note that %s is gitignored, so this input is not in the repository", err, liveClaudeTranscript)
	}
	meta := ClaudeMeta{
		Arm: ArmClaude, Task: "hono", Version: 1, Run: 1,
		CLIVersion: "2.1.278 (Claude Code)", CredentialKind: CredentialKindSubscription,
	}
	row, gaps, err := ParseClaude(data, meta)
	if err != nil {
		t.Fatalf("ParseClaude: %v", err)
	}
	if row.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want claude-opus-5: the plan asks for the opus alias and the row must carry what the response reported", row.Model)
	}
	if row.Turns < 2 {
		t.Errorf("Turns = %d: a full task is many turns, and a one-turn count means num_turns is not what this bench thinks it is", row.Turns)
	}
	if row.EndReason != EndReasonDone {
		t.Errorf("EndReason = %q, want done", row.EndReason)
	}
	if row.ModelDollars != nil {
		t.Errorf("a subscription run carries no model dollars, got %v", *row.ModelDollars)
	}
	if !gapsName(gaps, "model dollars") {
		t.Errorf("the transcript reports a total_cost_usd this row drops, and no gap names it: %v", gaps)
	}
	if row.ToolCalls != (ToolCalls{}) {
		t.Errorf("ToolCalls = %+v: --output-format json carries no tool counts, so a nonzero one came from somewhere else", row.ToolCalls)
	}
}

const liveClaudeTranscript = "../../.playground/transcripts/claude-v1-20260919T061918Z.txt"

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
