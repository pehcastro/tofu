package transfer

import (
	"fmt"

	"example.com/ledger/store"
)

type Transfer struct {
	From  string
	To    string
	Cents int64
}

func MoveAll(s *store.Store, transfers []Transfer) []error {
	var failed []error
	for i, t := range transfers {
		if err := Move(s, t.From, t.To, t.Cents); err != nil {
			failed = append(failed, fmt.Errorf("transfer %d: %w", i, err))
		}
	}
	return failed
}

func Exposure(s *store.Store, transfers []Transfer) (int64, error) {
	var total int64
	for _, t := range transfers {
		source, ok := s.Get(t.From)
		if !ok {
			return 0, fmt.Errorf("transfer: no account %q", t.From)
		}
		if source.Balance < t.Cents {
			total += t.Cents - source.Balance
		}
	}
	return total, nil
}
