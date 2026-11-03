package shipping

import (
	"errors"
	"fmt"

	"example.com/depot/retry"
)

var ErrTooHeavy = errors.New("shipping: parcel over the weight limit")

type Parcel struct {
	Order   string
	Grams   int
	Country string
}

type Dispatcher struct {
	carrier  Carrier
	attempts int
	sent     []string
}

func NewDispatcher(c Carrier) *Dispatcher {
	return &Dispatcher{carrier: c, attempts: carrierAttempts}
}

func (d *Dispatcher) Ship(p Parcel) (string, error) {
	if p.Grams > maxGrams {
		return "", fmt.Errorf("%w: %d g", ErrTooHeavy, p.Grams)
	}
	label := makeLabel(p)
	err := retry.Do(d.attempts, func() error {
		return d.carrier.Send(label)
	})
	if err != nil {
		return "", fmt.Errorf("shipping %s: %w", p.Order, err)
	}
	d.sent = append(d.sent, label.Tracking)
	return label.Tracking, nil
}

func (d *Dispatcher) Sent() []string {
	return append([]string(nil), d.sent...)
}
