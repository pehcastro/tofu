package shop

import (
	"fmt"
	"io"
)

func ExportCSV(w io.Writer, items []Item) error {
	if _, err := fmt.Fprintln(w, "sku,qty,price,line_total"); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(w, "%s,%d,%s,%s\n", item.SKU, item.Qty, Money(item.Price), Money(item.Price*item.Qty)); err != nil {
			return err
		}
	}
	return nil
}
