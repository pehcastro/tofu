package ledger

import (
	"fmt"
	"time"
)

type Stats struct {
	Rows    int
	Corrupt int
	Days    int
	First   time.Time
	Last    time.Time
}

func Summary(dir string) (Stats, error) {
	var stats Stats
	report, err := NewReader(dir).Each(Filter{}, func(row Row) error {
		if stats.First.IsZero() || row.At.Before(stats.First) {
			stats.First = row.At
		}
		if stats.Last.IsZero() || row.At.After(stats.Last) {
			stats.Last = row.At
		}
		return nil
	})
	if err != nil {
		return Stats{}, err
	}
	stats.Rows = report.Matched
	stats.Days = report.Files
	stats.Corrupt = len(report.Corrupt)
	return stats, nil
}

func (s Stats) String() string {
	if s.Rows == 0 {
		return "empty"
	}
	line := fmt.Sprintf("%d rows over %d days, %s to %s",
		s.Rows, s.Days, s.First.UTC().Format(dayLayout), s.Last.UTC().Format(dayLayout))
	if s.Corrupt > 0 {
		line += fmt.Sprintf(", %d unreadable lines", s.Corrupt)
	}
	return line
}
