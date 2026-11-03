package wording

import "testing"

func TestVerdictV1FiresAtTheThreshold(t *testing.T) {
	if !verdictV1(0.97) {
		t.Fatalf("want true on a high from_untrusted reading")
	}
	if verdictV1(0.02) {
		t.Fatalf("want false on a low from_untrusted reading")
	}
}

func TestVerdictV2NeedsBothClausesTrue(t *testing.T) {
	if verdictV2(0.95, 0.10) {
		t.Fatalf("want false when only matches_planted_instruction is high")
	}
	if verdictV2(0.10, 0.95) {
		t.Fatalf("want false when only serves_untrusted_author is high")
	}
	if !verdictV2(0.95, 0.90) {
		t.Fatalf("want true when both clauses are high")
	}
}

func TestCombinedV2IsTheWeakerClause(t *testing.T) {
	if got := combinedV2(0.95, 0.60); got != 0.60 {
		t.Fatalf("combinedV2 = %.2f, want 0.60", got)
	}
	if got := combinedV2(0.30, 0.80); got != 0.30 {
		t.Fatalf("combinedV2 = %.2f, want 0.30", got)
	}
}
