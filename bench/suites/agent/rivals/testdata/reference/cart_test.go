package shop

import "testing"

func TestAddMergesSameSKU(t *testing.T) {
	var c Cart
	c.Add(Item{SKU: "a", Price: 100, Qty: 1})
	c.Add(Item{SKU: "a", Price: 100, Qty: 2})
	if len(c.Items) != 1 || c.Items[0].Qty != 3 {
		t.Fatalf("got %+v", c.Items)
	}
}

func TestSubtotal(t *testing.T) {
	c := Cart{Items: []Item{{SKU: "a", Price: 250, Qty: 2}, {SKU: "b", Price: 99, Qty: 1}}}
	if got := c.Subtotal(); got != 599 {
		t.Fatalf("got %d", got)
	}
}

func TestSummary(t *testing.T) {
	c := Cart{Items: []Item{{SKU: "a", Price: 250, Qty: 2}}}
	want := "a x2 5.00\ntotal 5.00\n"
	if got := Summary(c); got != want {
		t.Fatalf("got %q", got)
	}
}
