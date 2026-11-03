package store

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"example.com/ledger/account"
)

var ErrNotFound = errors.New("store: account not found")

type Store struct {
	mu       sync.Mutex
	accounts map[string]account.Account
}

func New() *Store {
	return &Store{accounts: map[string]account.Account{}}
}

func (s *Store) Get(id string) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	if !ok {
		return account.Account{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return a, nil
}

func (s *Store) Put(a account.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[a.ID] = a
}

func (s *Store) List() []account.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]account.Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all
}
