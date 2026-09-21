package state

import "testing"

func TestStopCheckRuleDeclaresItsOwnSchemaAndLoadsThroughTheForeignPath(t *testing.T) {
	r, _, err := StopCheckRule()
	if err != nil {
		t.Fatalf("StopCheckRule: %v", err)
	}
	if r.Schema != stopCheckSchema {
		t.Fatalf("schema = %q, want %q", r.Schema, stopCheckSchema)
	}
	if len(r.ForeignThresholds) != 5 {
		t.Fatalf("ForeignThresholds has %d entries, want the 5 the yaml declares under thresholds: %v", len(r.ForeignThresholds), r.ForeignThresholds)
	}
	want := map[string]float64{
		"stop_pressure_ask_at":      1.5,
		"stop_pressure_deny_at":     2.5,
		"work_remains_relax_at":     0.85,
		"stalled_relax_at":          0.15,
		"budget_exhausted_block_at": 0.5,
	}
	for name, value := range want {
		got, ok := r.ForeignThresholds[name]
		if !ok || got != value {
			t.Fatalf("ForeignThresholds[%q] = %v, present %v, want %v", name, got, ok, value)
		}
	}
	if r.Thresholds.RiskAskAt != want["stop_pressure_ask_at"] || r.Thresholds.RiskDenyAt != want["stop_pressure_deny_at"] {
		t.Fatalf("the assembled Thresholds does not carry the foreign values through: %+v", r.Thresholds)
	}
	if r.RiskQuestion != stopCheckRiskQuestion || r.ApprovalQuestion != stopCheckApprovalQuestion ||
		r.UserRequestedQuestion != stopCheckUserRequestedQuestion || r.FromUntrustedQuestion != stopCheckFromUntrustedQuestion {
		t.Fatalf("the assembled role questions are wrong: %+v", r)
	}
}
