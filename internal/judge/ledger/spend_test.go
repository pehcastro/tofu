package ledger

import "testing"

func TestPlusKeepsMoneyAndListPriceInSeparateFields(t *testing.T) {
	total := Spend{Money: 0.000041, UnpricedCalls: 0}.Plus(Spend{List: 0.005351, UnpricedCalls: 2})
	if total.Money != 0.000041 {
		t.Fatalf("money moved: %v", total.Money)
	}
	if total.List != 0.005351 {
		t.Fatalf("list price moved: %v", total.List)
	}
	if total.UnpricedCalls != 2 {
		t.Fatalf("unpriced calls: %d, want 2", total.UnpricedCalls)
	}
}

func TestUnitNamesEveryVariant(t *testing.T) {
	want := []string{"money", "list price", "unpriced", "undetermined"}
	for i, name := range want {
		if got := Unit(i).String(); got != name {
			t.Errorf("Unit(%d) is %q, want %q", i, got, name)
		}
	}
}
