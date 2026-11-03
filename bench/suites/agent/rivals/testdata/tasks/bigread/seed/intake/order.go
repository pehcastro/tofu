package intake

import "errors"

type Class int

const (
	ClassStandard Class = iota
	ClassUrgent
	ClassBulk
)

func (c Class) String() string {
	switch c {
	case ClassStandard:
		return "standard"
	case ClassUrgent:
		return "urgent"
	case ClassBulk:
		return "bulk"
	}
	return "unknown"
}

var ErrEmpty = errors.New("intake: order has no lines")

type Line struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int64  `json:"unit_cents"`
}

type Order struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Region   string `json:"region"`
	Lines    []Line `json:"lines"`
	Class    Class  `json:"-"`
}

func (o Order) Total() int64 {
	var total int64
	for _, line := range o.Lines {
		total += int64(line.Quantity) * line.UnitCents
	}
	return total
}
