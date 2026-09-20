package strong

import "testing"

func TestClampHoldsTheLimitAndTheFloor(t *testing.T) {
	if got := Clamp(9, 4); got != 4 {
		t.Fatalf("Clamp(9, 4) = %d, want 4", got)
	}
	if got := Clamp(-1, 4); got != 0 {
		t.Fatalf("Clamp(-1, 4) = %d, want 0", got)
	}
}

func TestClampPassesTheEmptyCaseThrough(t *testing.T) {
	if got := Clamp(0, 0); got != 0 {
		t.Fatalf("Clamp(0, 0) = %d, want 0", got)
	}
}
