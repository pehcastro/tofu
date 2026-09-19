package ledger

import (
	"strings"
	"testing"
)

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

func TestPoolUnitsLineWarnsWhenAMeteredCredentialSitsBesideASubscription(t *testing.T) {
	line := PoolUnitsLine([]Unit{UnitMoney, UnitUnpriced})
	if !strings.Contains(line, "Warning: this pool mixes a metered credential with one that spends quota") {
		t.Fatalf("no warning in %q", line)
	}
	t.Logf("doctor would print: %s", line)
}

func TestPoolUnitsLineStaysQuietWhenEveryCredentialIsMetered(t *testing.T) {
	line := PoolUnitsLine([]Unit{UnitMoney, UnitMoney})
	if strings.Contains(line, "Warning") {
		t.Fatalf("warned on a pool that does not mix: %q", line)
	}
	if line != "spend units: 2 money" {
		t.Fatalf("got %q", line)
	}
}

func TestPoolUnitsLineStaysQuietWhenEveryCredentialIsASubscription(t *testing.T) {
	if line := PoolUnitsLine([]Unit{UnitUnpriced, UnitUnpriced}); strings.Contains(line, "Warning") {
		t.Fatalf("warned on a pool that does not mix: %q", line)
	}
}

func TestPoolUnitsLineOnAnEmptyPool(t *testing.T) {
	if line := PoolUnitsLine(nil); line != "spend units: none, no credential" {
		t.Fatalf("got %q", line)
	}
}
