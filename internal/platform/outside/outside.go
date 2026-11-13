package outside

import (
	"context"
	"slices"
	"sync"
	"time"
)

type Kind int

const (
	Network Kind = iota
	Model
	Jev
	Person
	Process
)

type Clock struct {
	parent *Clock
	now    func() time.Time
	mu     sync.Mutex
	waits  []wait
}

type wait struct {
	kind     Kind
	from, to time.Time
}

type clockKey struct{}

func Open(ctx context.Context, now func() time.Time) (context.Context, *Clock) {
	parent, _ := ctx.Value(clockKey{}).(*Clock)
	clock := &Clock{parent: parent, now: now}
	return context.WithValue(ctx, clockKey{}, clock), clock
}

func Wait(ctx context.Context, kind Kind) func() {
	clock, _ := ctx.Value(clockKey{}).(*Clock)
	if clock == nil {
		return func() {}
	}
	type mark struct {
		clock *Clock
		at    int
	}
	from := clock.now()
	var marks []mark
	for open := clock; open != nil; open = open.parent {
		open.mu.Lock()
		marks = append(marks, mark{open, len(open.waits)})
		open.waits = append(open.waits, wait{kind: kind, from: from})
		open.mu.Unlock()
	}
	var stopped sync.Once
	return func() {
		stopped.Do(func() {
			to := clock.now()
			for _, m := range marks {
				m.clock.mu.Lock()
				m.clock.waits[m.at].to = to
				m.clock.mu.Unlock()
			}
		})
	}
}

func (c *Clock) Outside() time.Duration {
	return c.merged(func(Kind) bool { return true })
}

func (c *Clock) OutsideOf(kinds ...Kind) time.Duration {
	return c.merged(func(kind Kind) bool { return slices.Contains(kinds, kind) })
}

func (c *Clock) merged(counts func(Kind) bool) time.Duration {
	now := c.now()
	c.mu.Lock()
	waits := slices.DeleteFunc(slices.Clone(c.waits), func(w wait) bool { return !counts(w.kind) })
	c.mu.Unlock()
	slices.SortFunc(waits, func(a, b wait) int { return a.from.Compare(b.from) })
	var total time.Duration
	var covered time.Time
	for _, w := range waits {
		if w.to.IsZero() {
			w.to = now
		}
		if w.from.Before(covered) {
			w.from = covered
		}
		if w.to.After(w.from) {
			total, covered = total+w.to.Sub(w.from), w.to
		}
	}
	return total
}
