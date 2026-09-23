package calibration

import (
	"sort"
	"time"
)

type Rate struct {
	Count    int
	Earliest time.Time
	Latest   time.Time
	Span     time.Duration
}

func RateFrom(times []time.Time) Rate {
	if len(times) == 0 {
		return Rate{}
	}
	sorted := make([]time.Time, len(times))
	copy(sorted, times)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	first, last := sorted[0], sorted[len(sorted)-1]
	return Rate{Count: len(sorted), Earliest: first, Latest: last, Span: last.Sub(first)}
}
