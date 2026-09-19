package harness

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fullRow() Row {
	dollars := 1.23
	start := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	return Row{
		Arm:            ArmClaude,
		Task:           "hono",
		Version:        1,
		Run:            1,
		Start:          start,
		End:            start.Add(90 * time.Second),
		WallClockMS:    90000,
		Model:          "claude-haiku-4-5-20251001",
		CLIVersion:     "2.1.277",
		CredentialKind: CredentialKindKey,
		BilledInput:    14696,
		BilledOutput:   44,
		ModelDollars:   &dollars,
		JudgeDollars:   0.000123,
		Turns:          3,
		ToolCalls:      ToolCalls{Read: 4, Write: 2, Edit: 1, Search: 1, Shell: 1, Other: 1, Failed: 1, Retried: 1},
		EndReason:      EndReasonDone,
		Commit:         "abc1234",
		Gates: []GateResult{
			{Name: "build", Status: GateStatusPassed, Reason: "ok"},
			{Name: "lint", Status: GateStatusFailed, Reason: "boom\nexit status 1"},
			{Name: "test", Status: GateStatusCouldNotEvaluate, Reason: "command not found: bun test"},
		},
		Checklist: []ChecklistResult{{Item: "returns 400 on missing field", Passed: true}},
	}
}

func TestRowRoundTripsThroughJSONWithoutLosingAField(t *testing.T) {
	want := fullRow()

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Row
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !got.Start.Equal(want.Start) {
		t.Fatalf("Start did not round trip: want %v got %v", want.Start, got.Start)
	}
	if !got.End.Equal(want.End) {
		t.Fatalf("End did not round trip: want %v got %v", want.End, got.End)
	}
	got.Start, got.End = want.Start, want.End

	if got.ModelDollars == nil || *got.ModelDollars != *want.ModelDollars {
		t.Fatalf("ModelDollars did not round trip: want %v got %v", *want.ModelDollars, got.ModelDollars)
	}
	got.ModelDollars = want.ModelDollars

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("row round trip lost a field:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestSubscriptionDollarsAreNullNotZero(t *testing.T) {
	subscription := fullRow()
	subscription.CredentialKind = CredentialKindSubscription
	subscription.ModelDollars = nil

	zero := 0.0
	key := fullRow()
	key.ModelDollars = &zero

	subEncoded, err := json.Marshal(subscription)
	if err != nil {
		t.Fatalf("marshal subscription row: %v", err)
	}
	if !strings.Contains(string(subEncoded), `"model_dollars":null`) {
		t.Fatalf("subscription row did not encode model dollars as null: %s", subEncoded)
	}

	keyEncoded, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("marshal key row: %v", err)
	}
	if !strings.Contains(string(keyEncoded), `"model_dollars":0`) {
		t.Fatalf("key row with a real zero spend did not encode model dollars as 0: %s", keyEncoded)
	}

	var decodedSub, decodedKey Row
	if err := json.Unmarshal(subEncoded, &decodedSub); err != nil {
		t.Fatalf("unmarshal subscription row: %v", err)
	}
	if err := json.Unmarshal(keyEncoded, &decodedKey); err != nil {
		t.Fatalf("unmarshal key row: %v", err)
	}

	if decodedSub.ModelDollars != nil {
		t.Fatalf("subscription row's model dollars should decode to nil, got %v", *decodedSub.ModelDollars)
	}
	if decodedSub.JudgeDollars != subscription.JudgeDollars {
		t.Fatalf("a subscription run still spends the openrouter key on jev, and that number was lost: want %v got %v", subscription.JudgeDollars, decodedSub.JudgeDollars)
	}
	if decodedKey.ModelDollars == nil {
		t.Fatal("key row's real zero spend decoded to nil, indistinguishable from a subscription arm")
	}
	if *decodedKey.ModelDollars != 0 {
		t.Fatalf("key row's model dollars should be 0, got %v", *decodedKey.ModelDollars)
	}
}

func TestARowStoredWithTheRetiredSpendEndReasonStillReads(t *testing.T) {
	encoded, err := json.Marshal(fullRow())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	stored := strings.Replace(string(encoded), `"end_reason":"done"`, `"end_reason":"dollar_cap"`, 1)
	if stored == string(encoded) {
		t.Fatal("the end reason is no longer stored under end_reason, this test is not reading what it claims")
	}

	var row Row
	if err := json.Unmarshal([]byte(stored), &row); err != nil {
		t.Fatalf("a harness row stored with the dollar cap end reason no longer reads: %v", err)
	}
	if row.EndReason != "dollar_cap" {
		t.Fatalf("EndReason = %q, want the stored dollar_cap kept verbatim", row.EndReason)
	}
	if row.Turns != 3 || row.Model == "" {
		t.Fatalf("the rest of the stored row did not survive: %+v", row)
	}
}
