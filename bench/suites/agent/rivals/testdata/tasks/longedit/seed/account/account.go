package account

import (
	"errors"
	"fmt"
)

var (
	ErrFrozen    = errors.New("account: frozen")
	ErrOverdrawn = errors.New("account: would go over the overdraft limit")
)

type Acct struct {
	ID      string
	Owner   string
	Balance int64
	Frozen  bool
}

func New(id, owner string) Acct {
	return Acct{ID: id, Owner: owner}
}

func (a Acct) Deposit(cents int64) (Acct, error) {
	if a.Frozen {
		return a, fmt.Errorf("%w: %s", ErrFrozen, a.ID)
	}
	a.Balance += cents
	return a, nil
}

func (a Acct) Withdraw(cents int64) (Acct, error) {
	if a.Frozen {
		return a, fmt.Errorf("%w: %s", ErrFrozen, a.ID)
	}
	if !CanWithdraw(a, cents) {
		return a, fmt.Errorf("%w: %s", ErrOverdrawn, a.ID)
	}
	a.Balance -= cents
	return a, nil
}
