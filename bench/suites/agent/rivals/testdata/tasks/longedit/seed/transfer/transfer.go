package transfer

import (
	"errors"
	"fmt"

	"example.com/ledger/store"
)

var ErrSameAccount = errors.New("transfer: from and to are the same account")

func Move(s *store.Store, from, to string, cents int64) error {
	if from == to {
		return ErrSameAccount
	}
	source, ok := s.Get(from)
	if !ok {
		return fmt.Errorf("transfer: no account %q", from)
	}
	target, ok := s.Get(to)
	if !ok {
		return fmt.Errorf("transfer: no account %q", to)
	}
	source, err := source.Withdraw(cents)
	if err != nil {
		return err
	}
	target, err = target.Deposit(cents)
	if err != nil {
		return err
	}
	s.Put(source)
	s.Put(target)
	return nil
}
