package report

import (
	"fmt"
	"strings"

	"example.com/ledger/account"
	"example.com/ledger/store"
)

func Statement(s *store.Store, id string) (string, error) {
	a, ok := s.Get(id)
	if !ok {
		return "", fmt.Errorf("report: no account %q", id)
	}
	return line(a), nil
}

func line(a account.Acct) string {
	text := fmt.Sprintf("%s (%s): %s", a.Owner, a.ID, Money(a.Balance))
	if a.Frozen {
		text += " frozen"
	}
	return text
}

func Totals(s *store.Store) string {
	var b strings.Builder
	var sum int64
	for _, a := range s.List() {
		b.WriteString(line(a) + "\n")
		sum += a.Balance
	}
	fmt.Fprintf(&b, "total: %s\n", Money(sum))
	return b.String()
}
