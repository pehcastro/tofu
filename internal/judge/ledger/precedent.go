package ledger

import (
	"math"
	"sort"

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
	byQuestion := make(map[string]Answer, len(left))
	for _, answer := range left {
		byQuestion[answer.Question] = answer
	}
	total := 0.0
	shared := 0
	for _, answer := range right {
		mine, ok := byQuestion[answer.Question]
		if !ok || mine.Kind != answer.Kind {
			continue
		}
		total += answerGap(mine, answer)
		shared++
	}
	if shared == 0 {
		return answersUnrelated, false
	}
	return total / float64(shared), true
}

func answerGap(left, right Answer) float64 {
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

func levelSpan(left, right Answer) float64 {
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
	for _, candidate := range candidates {
		if candidate.ID == target.ID || candidate.ReplayOf != "" || !candidate.At.Before(target.At) {
			continue
		}
		distance, comparable := AnswerDistance(target.Answers, candidate.Answers)
		same := target.Fingerprint != "" && candidate.Fingerprint == target.Fingerprint
		if !same && (!comparable || distance > precedentNearCeiling) {
			continue
		}
		found = append(found, Precedent{Row: candidate, Distance: distance, SameFingerprint: same, Comparable: comparable})
	}
	sort.SliceStable(found, func(i, j int) bool { return ranksAhead(found[i], found[j]) })
	if len(found) > precedentShortlistMax {
		found = found[:precedentShortlistMax]
	}
	return found
}

func ranksAhead(a, b Precedent) bool {
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

func (r *Reader) Precedents(target Row) ([]Precedent, error) {
	var candidates []Row
	_, err := r.Each(Filter{Point: target.Point, Until: target.At}, func(row Row) error {
		candidates = append(candidates, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Shortlist(target, candidates), nil
}
