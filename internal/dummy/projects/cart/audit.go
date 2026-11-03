package shop

import "fmt"

type event struct {
	Kind string
	SKU  string
	Qty  int
}

var trail []event

func note(kind, sku string, qty int) {
	trail = append(trail, event{Kind: kind, SKU: sku, Qty: qty})
}

func describe(e event) string {
	return fmt.Sprintf("%s %s x%d", e.Kind, e.SKU, e.Qty)
}

func History() []string {
	lines := make([]string, 0, len(trail))
	for _, e := range trail {
		lines = append(lines, describe(e))
	}
	return lines
}
