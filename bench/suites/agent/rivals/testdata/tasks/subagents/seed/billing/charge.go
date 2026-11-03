package billing

import (
	"errors"

	"example.com/depot/retry"
)

var ErrDeclined = errors.New("billing: charge declined")

type Gateway interface {
	Authorize(c Charge) (string, error)
	Capture(authorization string) error
}

type Charge struct {
	Customer string
	Cents    int64
	Currency string
}

type Biller struct {
	gateway Gateway
	ledger  *Ledger
}

func NewBiller(g Gateway, l *Ledger) *Biller {
	return &Biller{gateway: g, ledger: l}
}

func (b *Biller) Collect(c Charge) (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	var authorization string
	err := retry.Do(captureAttempts, func() error {
		id, err := b.gateway.Authorize(c)
		if err != nil {
			return err
		}
		authorization = id
		return b.gateway.Capture(id)
	})
	if err != nil {
		return "", errors.Join(ErrDeclined, err)
	}
	b.ledger.Record(c, authorization)
	return authorization, nil
}

func (b *Biller) Refund(c Charge, authorization string) {
	c.Cents = -c.Cents
	b.ledger.Record(c, authorization)
}
