package handler

import "testing"

func TestHandle(t *testing.T) {
	var orders []Order
	for id := range 40 {
		orders = append(orders, Order{ID: id, Items: []string{"tea", "rice", "tofu", "miso"}})
	}
	if Handle(orders) == "" {
		t.Fatal("no output")
	}
}
