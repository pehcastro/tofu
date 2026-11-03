package billing

import "testing"

type okGateway struct{}

func (okGateway) Authorize(Charge) (string, error) { return "auth-1", nil }
func (okGateway) Capture(string) error             { return nil }

func TestCollectRecords(t *testing.T) {
	ledger := &Ledger{}
	inv := Invoice{Customer: "c1", Lines: []Line{{Description: "box", Quantity: 2, UnitCents: 450}}}
	charge, err := inv.Charge()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBiller(okGateway{}, ledger).Collect(charge); err != nil {
		t.Fatal(err)
	}
	if got := ledger.Balance("c1"); got != 900 {
		t.Fatalf("balance %d", got)
	}
}
