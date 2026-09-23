package llm

import "testing"

func TestStopStringHandlesItsZeroValue(t *testing.T) {
	if got := StopUnknown.String(); got != "unknown" {
		t.Fatalf("StopUnknown.String() = %q, want %q", got, "unknown")
	}
}
