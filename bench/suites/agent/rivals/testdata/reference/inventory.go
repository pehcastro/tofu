package shop

import "errors"

var (
	ErrUnknownSKU   = errors.New("unknown sku")
	ErrInsufficient = errors.New("insufficient stock")
)

type Inventory struct {
	stock map[string]int
}

func NewInventory() *Inventory {
	return &Inventory{stock: map[string]int{}}
}

func (inv *Inventory) Add(sku string, qty int) {
	inv.stock[sku] += qty
	note("add", sku, qty)
}

func (inv *Inventory) Remove(sku string, qty int) error {
	have, known := inv.stock[sku]
	if !known {
		return ErrUnknownSKU
	}
	if qty > have {
		return ErrInsufficient
	}
	inv.stock[sku] = have - qty
	note("remove", sku, qty)
	return nil
}

func (inv *Inventory) Count(sku string) int {
	return inv.stock[sku]
}
