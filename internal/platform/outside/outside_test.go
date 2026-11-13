package outside_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"tofu/internal/platform/outside"
)

type fakeTime struct {
	mu sync.Mutex
	at time.Time
}

func (f *fakeTime) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.at
}

func (f *fakeTime) pass(ms int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at = f.at.Add(time.Duration(ms) * time.Millisecond)
}

func TestOutsideClock(t *testing.T) {
	cases := []struct {
		name  string
		drive func(clock *fakeTime, root context.Context) []*outside.Clock
		want  []int64
	}{
		{"two parallel waits over one span count once", func(clock *fakeTime, root context.Context) []*outside.Clock {
			ctx, wall := outside.Open(root, clock.now)
			first, second := outside.Wait(ctx, outside.Jev), outside.Wait(ctx, outside.Jev)
			clock.pass(40)
			first()
			clock.pass(10)
			second()
			clock.pass(5)
			return []*outside.Clock{wall}
		}, []int64{50}},
		{"disjoint waits leave the gap between them inside", func(clock *fakeTime, root context.Context) []*outside.Clock {
			ctx, wall := outside.Open(root, clock.now)
			stop := outside.Wait(ctx, outside.Network)
			clock.pass(10)
			stop()
			clock.pass(30)
			stop = outside.Wait(ctx, outside.Model)
			clock.pass(20)
			stop()
			return []*outside.Clock{wall}
		}, []int64{30}},
		{"a wait in a nested wall counts in every wall above it", func(clock *fakeTime, root context.Context) []*outside.Clock {
			outer, verb := outside.Open(root, clock.now)
			inner, step := outside.Open(outer, clock.now)
			innermost, call := outside.Open(inner, clock.now)
			stop := outside.Wait(innermost, outside.Process)
			clock.pass(25)
			stop()
			return []*outside.Clock{verb, step, call}
		}, []int64{25, 25, 25}},
		{"a parent wait does not leak into a child opened after it or a sibling", func(clock *fakeTime, root context.Context) []*outside.Clock {
			ctx, verb := outside.Open(root, clock.now)
			stop := outside.Wait(ctx, outside.Person)
			clock.pass(15)
			_, child := outside.Open(ctx, clock.now)
			sibling, other := outside.Open(ctx, clock.now)
			clock.pass(5)
			stop()
			late := outside.Wait(sibling, outside.Jev)
			clock.pass(7)
			late()
			return []*outside.Clock{verb, child, other}
		}, []int64{27, 0, 7}},
		{"a wait still open when the wall is read counts up to now", func(clock *fakeTime, root context.Context) []*outside.Clock {
			ctx, wall := outside.Open(root, clock.now)
			clock.pass(3)
			_ = outside.Wait(ctx, outside.Jev)
			clock.pass(12)
			return []*outside.Clock{wall}
		}, []int64{12}},
		{"a wait with no clock in its context records nothing and does not panic", func(clock *fakeTime, root context.Context) []*outside.Clock {
			stop := outside.Wait(root, outside.Jev)
			clock.pass(9)
			stop()
			_, wall := outside.Open(root, clock.now)
			return []*outside.Clock{wall}
		}, []int64{0}},
		{"a wait stopped twice counts once", func(clock *fakeTime, root context.Context) []*outside.Clock {
			ctx, wall := outside.Open(root, clock.now)
			stop := outside.Wait(ctx, outside.Network)
			clock.pass(8)
			stop()
			clock.pass(8)
			stop()
			return []*outside.Clock{wall}
		}, []int64{8}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clock := &fakeTime{at: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)}
			walls := c.drive(clock, context.Background())
			for at, wall := range walls {
				if got := wall.Outside().Milliseconds(); got != c.want[at] {
					t.Errorf("wall %d: outside %d ms, want %d", at, got, c.want[at])
				}
			}
		})
	}
}

func TestOutsideClockRace(t *testing.T) {
	ctx, wall := outside.Open(context.Background(), time.Now)
	var all sync.WaitGroup
	for range 16 {
		all.Go(func() {
			stop := outside.Wait(ctx, outside.Jev)
			_ = wall.Outside()
			stop()
		})
	}
	all.Wait()
	if wall.Outside() < 0 {
		t.Fatal("a negative outside time")
	}
}
