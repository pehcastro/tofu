package cost

import "testing"

func TestDecideProceedsWhenUserRequestedIsHighEvenIfApprovalIsHigh(t *testing.T) {
	if got := Decide(0.90, 0.94); got != Proceed {
		t.Fatalf("got %s, want proceed, the requested override should win", got)
	}
}

func TestDecideBlocksWhenApprovalIsHighAndTheUserDidNotAsk(t *testing.T) {
	if got := Decide(0.02, 0.95); got != Block {
		t.Fatalf("got %s, want block", got)
	}
}

func TestDecideProceedsWhenNeitherSignalFires(t *testing.T) {
	if got := Decide(0.17, 0.09); got != Proceed {
		t.Fatalf("got %s, want proceed", got)
	}
}
