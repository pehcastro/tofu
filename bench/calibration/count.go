package calibration

import (
	"time"

	"tofu/internal/judge/ledger"
)

type PointCounts struct {
	Rows           int
	Labelled       int
	LabelledByKind map[string]int
	NearThreshold  map[string]int
}

type Counts struct {
	TotalRows     int
	OutcomeByKind map[string]int
	OutcomeTimes  []time.Time
	Points        map[string]*PointCounts
}

func newPointCounts() *PointCounts {
	return &PointCounts{LabelledByKind: map[string]int{}, NearThreshold: map[string]int{}}
}

func Count(dir string) (Counts, error) {
	catalog := map[string]Point{}
	for _, point := range Points() {
		catalog[point.Name] = point
	}
	counts := Counts{OutcomeByKind: map[string]int{}, Points: map[string]*PointCounts{}}
	reader := ledger.NewReader(dir)
	_, err := reader.Each(ledger.Filter{}, func(row ledger.Row) error {
		counts.TotalRows++
		pc, ok := counts.Points[row.Point]
		if !ok {
			pc = newPointCounts()
			counts.Points[row.Point] = pc
		}
		pc.Rows++
		if row.Outcome == nil {
			return nil
		}
		counts.OutcomeByKind[row.Outcome.Kind]++
		counts.OutcomeTimes = append(counts.OutcomeTimes, row.Outcome.At)
		pc.Labelled++
		pc.LabelledByKind[row.Outcome.Kind]++
		point, ok := catalog[row.Point]
		if !ok {
			return nil
		}
		value, ok := valueFor(row, point.Question)
		if !ok {
			return nil
		}
		for _, threshold := range point.Thresholds {
			if Near(value, threshold.Value) {
				pc.NearThreshold[threshold.Name]++
			}
		}
		return nil
	})
	return counts, err
}
