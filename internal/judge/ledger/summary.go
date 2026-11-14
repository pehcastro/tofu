package ledger

import (
	"encoding/json"
	"errors"
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

type rowStamp struct {
	Schema  int           `json:"schema"`
	At      time.Time     `json:"at"`
	Verdict Verdict       `json:"verdict"`
	Reason  *reasonMode   `json:"reason"`
	Answers []answerShape `json:"answers"`
}

type reasonMode struct {
	Mode Mode `json:"mode"`
}

func (s rowStamp) readable() error {
	for _, answer := range s.Answers {
		if err := answer.readable(); err != nil {
			return err
		}
	}
	return readableSchema(s.Schema)
}

func Summary(dir string) (Stats, error) {
	reader := NewReader(dir)
	days, err := reader.days()
	if err != nil {
		return Stats{}, err
	}
	each, errs := make([]Stats, len(days)), make([]error, len(days))
	atOnce(len(days), func(i int) { each[i], errs[i] = reader.dayStats(days[i]) })
	if err := errors.Join(errs...); err != nil {
		return Stats{}, err
	}
	stats := Stats{Days: len(days)}
	for _, day := range each {
		stats.Rows, stats.Corrupt = stats.Rows+day.Rows, stats.Corrupt+day.Corrupt
		if day.Rows > 0 && (stats.First.IsZero() || day.First.Before(stats.First)) {
			stats.First = day.First
		}
		if day.Rows > 0 && (stats.Last.IsZero() || day.Last.After(stats.Last)) {
			stats.Last = day.Last
		}
	}
	return stats, nil
}

func (r *Reader) dayStats(day string) (Stats, error) {
	var stats Stats
	cut, err := eachLine(r.dayPath(day), func(_ int, line []byte) error {
		var stamp rowStamp
		if json.Unmarshal(line, &stamp) != nil || stamp.readable() != nil {
			stats.Corrupt++
			return nil
		}
		if stats.Rows == 0 || stamp.At.Before(stats.First) {
			stats.First = stamp.At
		}
		if stats.Rows == 0 || stamp.At.After(stats.Last) {
			stats.Last = stamp.At
		}
		stats.Rows++
		return nil
	})
	if cut != nil {
		stats.Corrupt++
	}
	return stats, err
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
