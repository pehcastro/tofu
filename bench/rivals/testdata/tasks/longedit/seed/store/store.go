package store

import (
	"sort"
	"sync"

	"example.com/ledger/account"
)

type Store struct {
	mu       sync.Mutex
	accounts map[string]account.Acct
}

func New() *Store {
	return &Store{accounts: map[string]account.Acct{}}
}

func (s *Store) Get(id string) (account.Acct, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	return a, ok
}

func (s *Store) Put(a account.Acct) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[a.ID] = a
}

func (s *Store) List() []account.Acct {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]account.Acct, 0, len(s.accounts))
	for _, a := range s.accounts {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all
}
