package account

import (
	"errors"
	"fmt"
)

var (
	ErrFrozen    = errors.New("account: frozen")
	ErrOverdrawn = errors.New("account: would go over the overdraft limit")
)

type Account struct {
	ID       string
	Owner    string
	Currency string
	Balance  int64
	Frozen   bool
}

func New(id, owner, currency string) Account {
	return Account{ID: id, Owner: owner, Currency: currency}
}

func (a Account) Deposit(cents int64) (Account, error) {
	if a.Frozen {
		return a, fmt.Errorf("%w: %s", ErrFrozen, a.ID)
	}
	a.Balance += cents
	return a, nil
}

func (a Account) Withdraw(cents int64) (Account, error) {
	if a.Frozen {
		return a, fmt.Errorf("%w: %s", ErrFrozen, a.ID)
	}
	if !CanWithdraw(a, cents) {
		return a, fmt.Errorf("%w: %s", ErrOverdrawn, a.ID)
	}
	a.Balance -= cents
	return a, nil
}
