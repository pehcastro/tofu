package ledger

import (
	"math"
	"slices"
	"sync"
	"time"

	"tofu/internal/konst"
)

const precedentShortlistMax = 10

const precedentNearCeiling = 2 * konst.ThresholdDeadBand

const answersUnrelated = 1.0

type Precedent struct {
	Row             Row
	Distance        float64
	SameFingerprint bool
	Comparable      bool
}

func AnswerDistance(left, right []Answer) (float64, bool) {
	total := 0.0
	shared := 0
	for i := range right {
		answer := &right[i]
		for j := range left {
			if mine := &left[j]; mine.Question == answer.Question {
				if mine.Kind == answer.Kind {
					total += answerGap(mine, answer)
					shared++
				}
				break
			}
		}
	}
	if shared == 0 {
		return answersUnrelated, false
	}
	return total / float64(shared), true
}

func answerGap(left, right *Answer) float64 {
	switch left.Kind {
	case AnswerNoul:
		return math.Abs(left.Noul - right.Noul)
	case AnswerScore:
		return math.Min(math.Abs(left.Score-right.Score)/levelSpan(left, right), answersUnrelated)
	case AnswerChoice:
		if left.Choice == right.Choice {
			return 0
		}
		return answersUnrelated
	}
	panic("ledger: unknown answer kind " + string(left.Kind))
}

func levelSpan(left, right *Answer) float64 {
	levels := len(left.Dist)
	if len(right.Dist) > levels {
		levels = len(right.Dist)
	}
	if levels < 2 {
		return 1
	}
	return float64(levels - 1)
}

func Shortlist(target Row, candidates []Row) []Precedent {
	var found []Precedent
	for i := range candidates {
		candidate := &candidates[i]
		if !candidate.At.Before(target.At) || candidate.ReplayOf != "" {
			continue
		}
		distance, comparable := AnswerDistance(target.Answers, candidate.Answers)
		same := target.Fingerprint != "" && candidate.Fingerprint == target.Fingerprint
		if !same && (!comparable || distance > precedentNearCeiling) {
			continue
		}
		near := Precedent{Row: *candidate, Distance: distance, SameFingerprint: same, Comparable: comparable}
		at := len(found)
		for j := range found {
			if ranksAhead(&near, &found[j]) {
				at = j
				break
			}
		}
		if at < precedentShortlistMax {
			found = slices.Insert(found, at, near)
			found = found[:min(len(found), precedentShortlistMax)]
		}
	}
	return found
}

func ranksAhead(a, b *Precedent) bool {
	if a.SameFingerprint != b.SameFingerprint {
		return a.SameFingerprint
	}
	if a.Distance != b.Distance {
		return a.Distance < b.Distance
	}
	if (a.Row.Outcome != nil) != (b.Row.Outcome != nil) {
		return a.Row.Outcome != nil
	}
	return a.Row.At.After(b.Row.At)
}

func (r *Reader) Precedents(targets []Row) ([][]Precedent, error) {
	points, until := map[string][]Row{}, time.Time{}
	for _, target := range targets {
		points[target.Point] = nil
		if target.At.After(until) {
			until = target.At
		}
	}
	_, err := r.Each(Filter{Until: until}, func(row Row) error {
		if candidates, wanted := points[row.Point]; wanted {
			row.State = nil
			points[row.Point] = append(candidates, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	found := make([][]Precedent, len(targets))
	var shortlisting sync.WaitGroup
	for i, target := range targets {
		shortlisting.Go(func() { found[i] = Shortlist(target, points[target.Point]) })
	}
	shortlisting.Wait()
	return found, nil
}
