package main

import (
	"fmt"

	"tofu/internal/judge/ledger"
)

func main() {
	total := ledger.Spend{Money: 0.000041}.Plus(ledger.Spend{List: 0.005351, UnpricedCalls: 1})
	fmt.Println(total.Money, total.List, total.UnpricedCalls)
}
