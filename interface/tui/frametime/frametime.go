package frametime

import (
	"runtime"
	"sort"
	"testing"
	"time"

	"tofu/internal/konst"
)

const (
	budget       = konst.FrameBudgetMicros * time.Microsecond
	stallCeiling = konst.FrameStallMicros * time.Microsecond
)

func Frames(t *testing.T, label string, render func()) {
	t.Helper()
	Samples(t, label, func() []time.Duration {
		taken := make([]time.Duration, 0, konst.FrameBudgetSamples)
		for range konst.FrameBudgetSamples {
			start := time.Now()
			render()
			taken = append(taken, time.Since(start))
		}
		return taken
	})
}

func Samples(t *testing.T, label string, take func() []time.Duration) {
	t.Helper()
	var best []time.Duration
	worstStall := time.Duration(0)
	for attempt := 1; attempt <= konst.FrameBudgetAttempts; attempt++ {
		stop := make(chan struct{})
		stalled := watchStall(stop)
		frames := take()
		close(stop)

		stall := <-stalled
		worstStall = max(worstStall, stall)
		if len(frames) == 0 {
			t.Fatalf("%s: no frames were measured", label)
		}
		if best == nil {
			best = frames
		} else if len(frames) != len(best) {
			t.Fatalf("%s: attempt %d measured %d frames where the first measured %d, so the attempts cannot be matched frame by frame",
				label, attempt, len(frames), len(best))
		}
		for index, frame := range frames {
			best[index] = min(best[index], frame)
		}
		median, worst := spread(frames)
		t.Logf("%s: attempt %d of %d, %d frames, median %v, worst %v, budget %v, an idle goroutine stalled %v",
			label, attempt, konst.FrameBudgetAttempts, len(frames), median, worst, budget, stall)
	}

	median, worst := spread(best)
	t.Logf("%s: the best of %d attempts taken frame by frame, median %v, worst %v, budget %v",
		label, konst.FrameBudgetAttempts, median, worst, budget)
	if median <= budget && worst <= budget {
		return
	}
	if worstStall > stallCeiling {
		t.Skipf("%s: the budget is not asserted on this run, median %v and worst %v were measured while an idle goroutine stalled %v, past the %v a machine with a core to spare shows, so the wall clock is measuring the scheduler rather than the renderer",
			label, median, worst, worstStall, stallCeiling)
	}
	t.Errorf("%s: median %v, worst %v, over the %v budget, taking the best of %d attempts frame by frame on a machine that stalled an idle goroutine only %v",
		label, median, worst, budget, konst.FrameBudgetAttempts, worstStall)
}

func spread(frames []time.Duration) (time.Duration, time.Duration) {
	sorted := append([]time.Duration{}, frames...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2], sorted[len(sorted)-1]
}

func watchStall(stop <-chan struct{}) <-chan time.Duration {
	stalled := make(chan time.Duration, 1)
	go func() {
		last, worst := time.Now(), time.Duration(0)
		for {
			select {
			case <-stop:
				stalled <- worst
				return
			default:
			}
			runtime.Gosched()
			now := time.Now()
			if gap := now.Sub(last); gap > worst {
				worst = gap
			}
			last = now
		}
	}()
	return stalled
}
