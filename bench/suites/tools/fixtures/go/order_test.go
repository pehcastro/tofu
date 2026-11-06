package orders

import "testing"

func TestTotal(t *testing.T) {
	got := Order{Quantity: 3, UnitCents: 250}.Total()
	want := 750
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestDiscounted(t *testing.T) {
	got := Order{Quantity: 3, UnitCents: 250}.Discounted()
	want := 675
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func TestShipping(t *testing.T) {
	t.Skip("shipping rates are not modelled yet")
}
