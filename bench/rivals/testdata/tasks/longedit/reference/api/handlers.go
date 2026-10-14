package api

import (
	"errors"
	"fmt"

	"example.com/ledger/account"
	"example.com/ledger/store"
	"example.com/ledger/transfer"
)

var ErrExists = errors.New("api: account already exists")

type Service struct {
	Store *store.Store
}

func (sv Service) Open(id, owner string) (account.Account, error) {
	if _, err := sv.Store.Get(id); err == nil {
		return account.Account{}, fmt.Errorf("%w: %s", ErrExists, id)
	}
	a := account.New(id, owner, "EUR")
	sv.Store.Put(a)
	return a, nil
}

func (sv Service) Balance(id string) (int64, error) {
	a, err := sv.Store.Get(id)
	if err != nil {
		return 0, fmt.Errorf("api: %w", err)
	}
	return a.Balance, nil
}

func (sv Service) Transfer(from, to string, cents int64) error {
	return transfer.Move(sv.Store, from, to, cents)
}
