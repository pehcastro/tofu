package mixed

import "testing"

func TestTotal(t *testing.T) {
	got := Total(3, 250)
	want := 750
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}
