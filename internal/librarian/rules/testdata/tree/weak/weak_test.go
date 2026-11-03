package weak

import "testing"

func TestSummariseReturnsAReport(t *testing.T) {
	got, err := Summarise([]string{"a"})
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}
	if got.Total == 0 {
		t.Fatal("Total is zero")
	}
}

func TestSummariseCountsWithAStubbedScale(t *testing.T) {
	scale = func() int { return 2 }
	got, err := Summarise([]string{"a", "b"})
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}
	if got.Total != 4 {
		t.Fatalf("Total = %d, want 4", got.Total)
	}
}
