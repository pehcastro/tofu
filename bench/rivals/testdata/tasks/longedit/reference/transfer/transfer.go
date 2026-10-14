package transfer

import (
	"errors"
	"fmt"

	"example.com/ledger/store"
)

var (
	ErrSameAccount = errors.New("transfer: from and to are the same account")
	ErrCurrency    = errors.New("transfer: accounts hold different currencies")
)

func Move(s *store.Store, from, to string, cents int64) error {
	if from == to {
		return ErrSameAccount
	}
	source, err := s.Get(from)
	if err != nil {
		return fmt.Errorf("transfer: %w", err)
	}
	target, err := s.Get(to)
	if err != nil {
		return fmt.Errorf("transfer: %w", err)
	}
	if source.Currency != target.Currency {
		return fmt.Errorf("%w: %s and %s", ErrCurrency, source.Currency, target.Currency)
	}
	source, err = source.Withdraw(cents)
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
