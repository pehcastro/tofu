package api

import (
	"errors"
	"fmt"

	"example.com/ledger/account"
	"example.com/ledger/store"
)

var ErrExists = errors.New("api: account already exists")

type Service struct {
	Store *store.Store
}

func (sv Service) Open(id, owner string) (account.Acct, error) {
	if _, ok := sv.Store.Get(id); ok {
		return account.Acct{}, fmt.Errorf("%w: %s", ErrExists, id)
	}
	a := account.New(id, owner)
	sv.Store.Put(a)
	return a, nil
}

func (sv Service) Balance(id string) (int64, error) {
	a, ok := sv.Store.Get(id)
	if !ok {
		return 0, fmt.Errorf("api: no account %q", id)
	}
	return a.Balance, nil
}
