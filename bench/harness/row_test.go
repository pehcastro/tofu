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
		Dollars:        &dollars,
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

	if got.Dollars == nil || *got.Dollars != *want.Dollars {
		t.Fatalf("Dollars did not round trip: want %v got %v", *want.Dollars, got.Dollars)
	}
	got.Dollars = want.Dollars

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("row round trip lost a field:\nwant %+v\ngot  %+v", want, got)
	}
}

func TestSubscriptionDollarsAreNullNotZero(t *testing.T) {
	subscription := fullRow()
	subscription.CredentialKind = CredentialKindSubscription
	subscription.Dollars = nil

	zero := 0.0
	key := fullRow()
	key.Dollars = &zero

	subEncoded, err := json.Marshal(subscription)
	if err != nil {
		t.Fatalf("marshal subscription row: %v", err)
	}
	if !strings.Contains(string(subEncoded), `"dollars":null`) {
		t.Fatalf("subscription row did not encode dollars as null: %s", subEncoded)
	}

	keyEncoded, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("marshal key row: %v", err)
	}
	if !strings.Contains(string(keyEncoded), `"dollars":0`) {
		t.Fatalf("key row with a real zero spend did not encode dollars as 0: %s", keyEncoded)
	}

	var decodedSub, decodedKey Row
	if err := json.Unmarshal(subEncoded, &decodedSub); err != nil {
		t.Fatalf("unmarshal subscription row: %v", err)
	}
	if err := json.Unmarshal(keyEncoded, &decodedKey); err != nil {
		t.Fatalf("unmarshal key row: %v", err)
	}

	if decodedSub.Dollars != nil {
		t.Fatalf("subscription row's dollars should decode to nil, got %v", *decodedSub.Dollars)
	}
	if decodedKey.Dollars == nil {
		t.Fatal("key row's real zero spend decoded to nil, indistinguishable from a subscription arm")
	}
	if *decodedKey.Dollars != 0 {
		t.Fatalf("key row's dollars should be 0, got %v", *decodedKey.Dollars)
	}
}
