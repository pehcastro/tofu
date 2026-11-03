package billing

import (
	"errors"
	"fmt"
	"strings"
)

const (
	maxLines        = 50
	captureAttempts = 3
	defaultCurrency = "EUR"
)

var ErrInvalid = errors.New("billing: invalid charge")

type Line struct {
	Description string
	Quantity    int
	UnitCents   int64
}

type Invoice struct {
	Customer string
	Currency string
	Lines    []Line
}

func (inv Invoice) Total() int64 {
	var total int64
	for _, line := range inv.Lines {
		total += int64(line.Quantity) * line.UnitCents
	}
	return total
}

func (inv Invoice) Charge() (Charge, error) {
	if len(inv.Lines) == 0 || len(inv.Lines) > maxLines {
		return Charge{}, fmt.Errorf("%w: %d lines", ErrInvalid, len(inv.Lines))
	}
	currency := inv.Currency
	if currency == "" {
		currency = defaultCurrency
	}
	return Charge{Customer: inv.Customer, Cents: inv.Total(), Currency: currency}, nil
}

func (inv Invoice) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invoice for %s\n", inv.Customer)
	for _, line := range inv.Lines {
		fmt.Fprintf(&b, "%3d x %-20s %8.2f\n", line.Quantity, line.Description, float64(line.UnitCents)/100)
	}
	fmt.Fprintf(&b, "total %.2f %s\n", float64(inv.Total())/100, inv.Currency)
	return b.String()
}

func (c Charge) validate() error {
	if c.Customer == "" || c.Cents <= 0 {
		return ErrInvalid
	}
	return nil
}
