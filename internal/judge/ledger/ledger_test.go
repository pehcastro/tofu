package ledger

import (
	"encoding/json"
	"testing"
)

func TestVerdictStringFailsOnAnUnknownValue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unknown verdict did not panic")
		}
	}()
	_ = Verdict("bogus").String()
}

func TestModeStringFailsOnAnUnknownValue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unknown mode did not panic")
		}
	}()
	_ = Mode("bogus").String()
}

func TestVerdictUnmarshalRefusesAnUnknownValue(t *testing.T) {
	var v Verdict
	err := json.Unmarshal([]byte(`"bogus"`), &v)
	if err == nil {
		t.Fatal("an unknown verdict decoded without error")
	}
	want := `ledger: unknown verdict "bogus"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestModeUnmarshalRefusesAnUnknownValue(t *testing.T) {
	var m Mode
	err := json.Unmarshal([]byte(`"bogus"`), &m)
	if err == nil {
		t.Fatal("an unknown mode decoded without error")
	}
	want := `ledger: unknown mode "bogus"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestVerdictAndModeUnmarshalAcceptEveryKnownValue(t *testing.T) {
	for _, v := range AllVerdicts() {
		var got Verdict
		encoded, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal %v: %v", v, err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatalf("Unmarshal %v: %v", v, err)
		}
		if got != v {
			t.Fatalf("got %v, want %v", got, v)
		}
	}
	for _, m := range AllModes() {
		var got Mode
		encoded, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("Marshal %v: %v", m, err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatalf("Unmarshal %v: %v", m, err)
		}
		if got != m {
			t.Fatalf("got %v, want %v", got, m)
		}
	}
}

func TestRowCarriesVerdictPolicyAndReasonThroughWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	written, err := NewWriter(dir).Append(Row{
		Point:         "tool_gate",
		Questions:     "tool_gate",
		Version:       1,
		Verdict:       VerdictDeny,
		Policy:        "tool_gate",
		PolicyVersion: 1,
		Reason: &Reason{
			Question:   "risk",
			Comparison: "risk_deny_at",
			Threshold:  2.5,
			Value:      3.0,
			Mode:       ModeEnforced,
		},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	found, ok, err := NewReader(dir).ByID(written.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !ok {
		t.Fatal("the written row was not found")
	}
	if found.Verdict != VerdictDeny || found.Policy != "tool_gate" || found.PolicyVersion != 1 {
		t.Fatalf("verdict/policy/policy_version = %v/%v/%v", found.Verdict, found.Policy, found.PolicyVersion)
	}
	if found.Reason == nil || found.Reason.Comparison != "risk_deny_at" || found.Reason.Threshold != 2.5 {
		t.Fatalf("reason = %+v", found.Reason)
	}
}

func TestRowRecordsItsModeAndReadsBackForBothModes(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	shadow, err := writer.Append(Row{
		Point: "tool_gate", Questions: "tool_gate", Version: 1,
		Verdict: VerdictAsk, Reason: &Reason{Question: "risk", Comparison: "risk_ask_at", Mode: ModeShadow},
	})
	if err != nil {
		t.Fatalf("Append shadow: %v", err)
	}
	enforced, err := writer.Append(Row{
		Point: "tool_gate", Questions: "tool_gate", Version: 1,
		Verdict: VerdictDeny, Reason: &Reason{Question: "risk", Comparison: "risk_deny_at", Mode: ModeEnforced},
	})
	if err != nil {
		t.Fatalf("Append enforced: %v", err)
	}

	foundShadow, ok, err := NewReader(dir).ByID(shadow.ID)
	if err != nil || !ok {
		t.Fatalf("ByID shadow: ok=%v err=%v", ok, err)
	}
	if foundShadow.Mode() != ModeShadow {
		t.Fatalf("shadow row mode = %v, want shadow", foundShadow.Mode())
	}

	foundEnforced, ok, err := NewReader(dir).ByID(enforced.ID)
	if err != nil || !ok {
		t.Fatalf("ByID enforced: ok=%v err=%v", ok, err)
	}
	if foundEnforced.Mode() != ModeEnforced {
		t.Fatalf("enforced row mode = %v, want enforced", foundEnforced.Mode())
	}
}

func TestRowWithNoVerdictCarriesNoPolicyFields(t *testing.T) {
	dir := t.TempDir()
	written, err := NewWriter(dir).Append(Row{Point: "tool_gate", Questions: "tool_gate", Version: 1})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	found, ok, err := NewReader(dir).ByID(written.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !ok {
		t.Fatal("the written row was not found")
	}
	if found.Verdict != VerdictUnset || found.Policy != "" || found.Reason != nil {
		t.Fatalf("expected no verdict, policy or reason, got verdict=%q policy=%q reason=%+v", found.Verdict, found.Policy, found.Reason)
	}
}
