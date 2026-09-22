package harness

import (
	"fmt"
	"sort"
)

type Spread struct {
	Median  float64
	Low     float64
	High    float64
	Repeats int
}

func SpreadOf(values []float64) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	median := sorted[middle]
	if len(sorted)%2 == 0 {
		median = (sorted[middle-1] + sorted[middle]) / 2
	}
	return Spread{Median: median, Low: sorted[0], High: sorted[len(sorted)-1], Repeats: len(sorted)}
}

func (s Spread) Width() float64 { return s.High - s.Low }

func (s Spread) Separable() bool { return s.Repeats > 1 }

func (s Spread) Line(unit string) string {
	switch s.Repeats {
	case 0:
		return "not recorded on any passing repeat"
	case 1:
		return fmt.Sprintf(unit+" on one repeat, which is a sample and not a spread", s.Median)
	}
	return fmt.Sprintf("median "+unit+", range "+unit+" to "+unit+" over %d repeats", s.Median, s.Low, s.High, s.Repeats)
}

type Stability struct {
	Arm     Arm
	Version int
	Passed  int
	Repeats int
}

func (s Stability) Unstable() bool { return s.Passed > 0 && s.Passed < s.Repeats }

func (s Stability) Line() string {
	return fmt.Sprintf("%s v%d passed %d of %d repeats, so the same task and the same arm disagree with themselves",
		s.Arm, s.Version, s.Passed, s.Repeats)
}

func StabilityOf(rows []Row) []Stability {
	type armVersion struct {
		Arm     Arm
		Version int
	}
	counted := map[armVersion]Stability{}
	for _, r := range rows {
		key := armVersion{r.Arm, r.Version}
		seen, held := counted[key]
		if !held {
			seen = Stability{Arm: r.Arm, Version: r.Version}
		}
		seen.Repeats++
		if Passed(r) {
			seen.Passed++
		}
		counted[key] = seen
	}
	out := make([]Stability, 0, len(counted))
	for _, s := range counted {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Arm != out[j].Arm {
			return out[i].Arm < out[j].Arm
		}
		return out[i].Version < out[j].Version
	})
	return out
}

type MissingField struct {
	Arm     Arm
	Version int
	Run     int
	Field   string
}

func (m MissingField) Line() string {
	name := string(m.Arm)
	if name == "" {
		name = "an unnamed arm"
	}
	return fmt.Sprintf("%s v%d run%d has no %s", name, m.Version, m.Run, m.Field)
}

func missingFieldOf(r Row) string {
	switch {
	case r.Arm == "":
		return "arm"
	case r.Task == "":
		return "task"
	case r.Run == 0:
		return "repeat index"
	case r.CredentialKind == "":
		return "credential kind"
	case len(r.Gates) == 0:
		return "gate result"
	}
	return ""
}

func readable(rows []Row) (kept []Row, missing []MissingField) {
	for _, r := range rows {
		field := missingFieldOf(r)
		if field == "" {
			kept = append(kept, r)
			continue
		}
		missing = append(missing, MissingField{Arm: r.Arm, Version: r.Version, Run: r.Run, Field: field})
	}
	return kept, missing
}
