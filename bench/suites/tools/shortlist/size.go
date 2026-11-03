package shortlist

import "math"

const (
	zTwoSided95 = 1.96
	zPower80    = 0.84
)

func laplaceRate(hits, total int) float64 {
	return float64(hits+1) / float64(total+2)
}

func NeededQuestions(hitsA, totalA, hitsB, totalB int) int {
	rateA := laplaceRate(hitsA, totalA)
	rateB := laplaceRate(hitsB, totalB)
	gap := rateA - rateB
	if gap < 0 {
		gap = -gap
	}
	variance := rateA*(1-rateA) + rateB*(1-rateB)
	z := zTwoSided95 + zPower80
	return int(math.Ceil(z * z * variance / (gap * gap)))
}
