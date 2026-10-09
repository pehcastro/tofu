package ledger

import (
	"maps"
	"slices"
	"time"
)

const weekDays = 7

type Threshold struct {
	Comparison string  `json:"comparison"`
	Value      float64 `json:"value"`
}

type PointSummary struct {
	Point      string        `json:"point"`
	Mode       Mode          `json:"mode"`
	Count      int           `json:"count"`
	Week       [weekDays]int `json:"week"`
	WouldAsk   int           `json:"would_ask"`
	Labeled    int           `json:"labeled"`
	Agreed     int           `json:"agreed"`
	MeanMS     int64         `json:"mean_ms"`
	CostUSD    float64       `json:"cost_usd"`
	Thresholds []Threshold   `json:"thresholds"`
}

type pointTally struct {
	PointSummary
	newest    time.Time
	latencyMS int64
	compared  map[string]Row
}

func (r *Reader) Points(since, now time.Time) ([]PointSummary, Report, error) {
	tallies := map[string]*pointTally{}
	today := localDay(now, now)
	report, err := r.Each(Filter{Since: since, Until: now}, func(row Row) error {
		tally := tallies[row.Point]
		if tally == nil {
			tally = &pointTally{PointSummary: PointSummary{Point: row.Point}, compared: map[string]Row{}}
			tallies[row.Point] = tally
		}
		tally.Count++
		tally.latencyMS += row.LatencyMS
		tally.CostUSD += row.Cost
		if ago := int(today.Sub(localDay(row.At, now)).Hours() / 24); ago < weekDays {
			tally.Week[weekDays-1-ago]++
		}
		if row.Mode() == ModeShadow && row.Verdict != VerdictAllow {
			tally.WouldAsk++
		}
		if row.Outcome != nil && slices.Contains([]Verdict{VerdictAllow, VerdictAsk, VerdictDeny}, Verdict(row.Outcome.Detail)) {
			tally.Labeled++
			if Verdict(row.Outcome.Detail) == row.Verdict {
				tally.Agreed++
			}
		}
		if !row.At.Before(tally.newest) {
			tally.newest, tally.Mode = row.At, row.Mode()
		}
		if row.Reason != nil && !row.At.Before(tally.compared[row.Reason.Comparison].At) {
			tally.compared[row.Reason.Comparison] = row
		}
		return nil
	})
	if err != nil {
		return nil, report, err
	}
	points := make([]PointSummary, 0, len(tallies))
	for _, name := range slices.Sorted(maps.Keys(tallies)) {
		tally := tallies[name]
		tally.MeanMS, tally.Thresholds = tally.latencyMS/int64(tally.Count), []Threshold{}
		for _, comparison := range slices.Sorted(maps.Keys(tally.compared)) {
			tally.Thresholds = append(tally.Thresholds, Threshold{Comparison: comparison, Value: tally.compared[comparison].Reason.Threshold})
		}
		points = append(points, tally.PointSummary)
	}
	return points, report, nil
}

func localDay(at, now time.Time) time.Time {
	year, month, day := at.In(now.Location()).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
