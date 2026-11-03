package shop

import (
	"fmt"
	"strings"
)

func Money(cents int) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func Summary(c Cart) string {
	var b strings.Builder
	for _, item := range c.Items {
		fmt.Fprintf(&b, "%s x%d %s\n", item.SKU, item.Qty, Money(item.Price*item.Qty))
	}
	fmt.Fprintf(&b, "total %s\n", Money(c.Subtotal()))
	return b.String()
}
