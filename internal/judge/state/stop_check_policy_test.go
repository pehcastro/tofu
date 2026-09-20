package state

import "testing"

func TestStopCheckPolicyDeclaresItsOwnSchemaAndLoadsThroughTheForeignPath(t *testing.T) {
	pol, _, err := StopCheckPolicy()
	if err != nil {
		t.Fatalf("StopCheckPolicy: %v", err)
	}
	if pol.Schema != stopCheckSchema {
		t.Fatalf("schema = %q, want %q", pol.Schema, stopCheckSchema)
	}
	if len(pol.ForeignThresholds) != 5 {
		t.Fatalf("ForeignThresholds has %d entries, want the 5 the yaml declares under thresholds: %v", len(pol.ForeignThresholds), pol.ForeignThresholds)
	}
	want := map[string]float64{
		"stop_pressure_ask_at":      1.5,
		"stop_pressure_deny_at":     2.5,
		"work_remains_relax_at":     0.85,
		"stalled_relax_at":          0.15,
		"budget_exhausted_block_at": 0.5,
	}
	for name, value := range want {
		got, ok := pol.ForeignThresholds[name]
		if !ok || got != value {
			t.Fatalf("ForeignThresholds[%q] = %v, present %v, want %v", name, got, ok, value)
		}
	}
	if pol.Thresholds.RiskAskAt != want["stop_pressure_ask_at"] || pol.Thresholds.RiskDenyAt != want["stop_pressure_deny_at"] {
		t.Fatalf("the assembled Thresholds does not carry the foreign values through: %+v", pol.Thresholds)
	}
	if pol.RiskQuestion != stopCheckRiskQuestion || pol.ApprovalQuestion != stopCheckApprovalQuestion ||
		pol.UserRequestedQuestion != stopCheckUserRequestedQuestion || pol.FromUntrustedQuestion != stopCheckFromUntrustedQuestion {
		t.Fatalf("the assembled role questions are wrong: %+v", pol)
	}
}
