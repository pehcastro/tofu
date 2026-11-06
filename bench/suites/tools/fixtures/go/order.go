package orders

import pr "fixture.test/orders/pricing"

type Order struct {
	Quantity  int
	UnitCents int
}

func (o Order) Total() int {
	return pr.Total(o.Quantity, o.UnitCents)
}

func (o Order) Discounted() int {
	return o.Total() - 100
}
