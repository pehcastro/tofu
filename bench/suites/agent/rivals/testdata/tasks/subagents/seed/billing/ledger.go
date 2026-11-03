package billing

import "sync"

type Entry struct {
	Customer      string
	Cents         int64
	Authorization string
}

type Ledger struct {
	mu      sync.Mutex
	entries []Entry
}

func (l *Ledger) Record(c Charge, authorization string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, Entry{Customer: c.Customer, Cents: c.Cents, Authorization: authorization})
}

func (l *Ledger) Balance(customer string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var total int64
	for _, e := range l.entries {
		if e.Customer == customer {
			total += e.Cents
		}
	}
	return total
}

func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.entries...)
}
