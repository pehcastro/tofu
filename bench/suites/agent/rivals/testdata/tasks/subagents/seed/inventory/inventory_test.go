package inventory

import "testing"

type fixedSource map[string]int

func (f fixedSource) Level(sku string) (int, error) { return f[sku], nil }

func TestPullAllMarksLow(t *testing.T) {
	stock := NewStock()
	if failed := NewSyncer(fixedSource{"a": 2, "b": 10}, stock).PullAll([]string{"a", "b"}); len(failed) != 0 {
		t.Fatal(failed)
	}
	if low := stock.Low(); len(low) != 1 || low[0] != "a" {
		t.Fatalf("low %v", low)
	}
}
