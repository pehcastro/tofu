package stat

import "testing"

func TestPercentileAgainstAHandCheckedTable(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		p      float64
		want   float64
	}{
		{"empty", nil, 50, 0},
		{"single value at any percentile", []float64{5}, 99, 5},
		{"two values, median", []float64{10, 20}, 50, 15},
		{"two values, p95", []float64{10, 20}, 95, 19.5},
		{"three distinct values, median", []float64{10, 20, 30}, 50, 20},
		{"three distinct values, p95", []float64{10, 20, 30}, 95, 29},
		{"three distinct values, p99", []float64{10, 20, 30}, 99, 29.8},
		{"three samples, p95 is the maximum", []float64{10, 20, 20}, 95, 20},
		{"three samples, p99 is the maximum", []float64{10, 20, 20}, 99, 20},
		{"unsorted input, median", []float64{30, 10, 20}, 50, 20},
	}
	for _, c := range cases {
		if got := Percentile(c.values, c.p); got != c.want {
			t.Errorf("%s: Percentile(%v, %v) = %v, want %v", c.name, c.values, c.p, got, c.want)
		}
	}
}

func TestMedianAgainstAHandCheckedValue(t *testing.T) {
	values := []float64{10, 20, 30}
	if got := Median(values); got != 20 {
		t.Fatalf("Median(%v) = %v, want 20", values, got)
	}
}

func TestSpreadAgainstAHandCheckedTable(t *testing.T) {
	cases := []struct {
		name     string
		values   []float64
		min, max float64
	}{
		{"empty", nil, 0, 0},
		{"single value", []float64{7}, 7, 7},
		{"three samples tied top two", []float64{10, 20, 20}, 10, 20},
	}
	for _, c := range cases {
		min, max := Spread(c.values)
		if min != c.min || max != c.max {
			t.Errorf("%s: Spread(%v) = (%v, %v), want (%v, %v)", c.name, c.values, min, max, c.min, c.max)
		}
	}
}
