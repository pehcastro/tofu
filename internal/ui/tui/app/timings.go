package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/konst"
	"tofu/internal/store/walls"
)

type timed int

const (
	timedFrame timed = iota
	timedKey
	timedAbsorb
	timedSend
	timedKinds
)

const bucketWidth = konst.TUIWallBucketMillis * time.Millisecond

type timings struct {
	buckets [timedKinds][konst.TUIWallBuckets]int
	count   [timedKinds]int
	longest [timedKinds]time.Duration
	since   time.Time
	keyAt   time.Time
}

func (t *timings) add(kind timed, began time.Time) {
	took := time.Since(began)
	t.buckets[kind][min(int(took/bucketWidth), konst.TUIWallBuckets-1)]++
	t.count[kind]++
	t.longest[kind] = max(t.longest[kind], took)
}

func (t *timings) median(kind timed) time.Duration {
	rank := (t.count[kind] - 1) / 2
	for bucket, counted := range t.buckets[kind] {
		if rank < counted {
			return time.Duration(bucket) * bucketWidth
		}
		rank -= counted
	}
	return t.longest[kind]
}

func (t *timings) summary() []walls.Wall {
	ops := [timedKinds]walls.Op{walls.OpFrame, walls.OpKey, walls.OpAbsorb, walls.OpSend}
	var rows []walls.Wall
	for kind := range timedKinds {
		if t.count[kind] == 0 {
			continue
		}
		rows = append(rows, walls.Wall{Op: ops[kind], Name: "tui", MS: t.median(kind).Milliseconds(), At: t.since.UnixMilli(), OK: true,
			OutsideMS: new(int64), N: t.count[kind], MaxMS: t.longest[kind].Milliseconds()})
	}
	*t = timings{since: time.Now(), keyAt: t.keyAt}
	return rows
}

func (a *App) wallsDue() tea.Cmd {
	rows, write := a.timings.summary(), a.options.Walls
	if len(rows) == 0 {
		return nil
	}
	return func() tea.Msg {
		write(rows)
		return nil
	}
}
