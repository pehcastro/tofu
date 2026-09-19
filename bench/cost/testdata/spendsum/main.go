package main

import (
	"fmt"

	"boji/internal/judge/ledger"
)

func main() {
	actual := ledger.Money(0.000041)
	list := ledger.ListPrice(0.005351)
	fmt.Println(actual + list)
}
