package jev

import (
	"testing"

	"boji/internal/transport"
)

func TestSpendMeterReservesTheWorstCase(t *testing.T) {
	meter := NewSpendMeter(0.001)
	if err := meter.Reserve(0.0009); err != nil {
		t.Fatalf("the first reservation should fit: %v", err)
	}
	err := meter.Reserve(0.0009)
	if transport.KindOf(err) != transport.KindBudget {
		t.Fatalf("expected kind budget on the second reservation, got %v", err)
	}
	meter.Settle(0.0009, 0.00001)
	if meter.Spent() != 0.00001 {
		t.Fatalf("expected the real cost, got %v", meter.Spent())
	}
	if err := meter.Reserve(0.0009); err != nil {
		t.Fatalf("the reservation should fit once the first settled: %v", err)
	}
}

func TestSpendMeterReleaseFreesTheHold(t *testing.T) {
	meter := NewSpendMeter(0.002)
	if err := meter.Reserve(0.001); err != nil {
		t.Fatalf("reserving: %v", err)
	}
	if meter.Remaining() != 0.001 {
		t.Fatalf("expected the hold to count against the limit, remaining is %v", meter.Remaining())
	}
	meter.Release(0.001)
	if meter.Remaining() != 0.002 {
		t.Fatalf("expected the hold released, remaining is %v", meter.Remaining())
	}
	if meter.Spent() != 0 {
		t.Fatalf("a released hold is not spend, got %v", meter.Spent())
	}
}
