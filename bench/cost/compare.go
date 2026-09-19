package cost

import "math"

const separationAlpha = 0.05

func compare(arms []ArmResult) []Pair {
	var pairs []Pair
	for i := 0; i < len(arms); i++ {
		for j := i + 1; j < len(arms); j++ {
			pairs = append(pairs, mcNemar(arms[i], arms[j]))
		}
	}
	return pairs
}

func mcNemar(left, right ArmResult) Pair {
	rightByCase := make(map[string]bool, len(right.Cases))
	for _, c := range right.Cases {
		rightByCase[c.Case] = c.Correct
	}
	pair := Pair{Left: left.Arm, Right: right.Arm}
	for _, c := range left.Cases {
		other, ok := rightByCase[c.Case]
		if !ok {
			continue
		}
		if c.Correct && !other {
			pair.LeftOnly++
		}
		if !c.Correct && other {
			pair.RightOnly++
		}
	}
	pair.Difference = pair.LeftOnly - pair.RightOnly
	pair.P = twoSidedSignP(pair.LeftOnly, pair.RightOnly)
	pair.SeparatedAt05 = pair.P < separationAlpha
	return pair
}

func twoSidedSignP(left, right int) float64 {
	discordant := left + right
	if discordant == 0 {
		return 1
	}
	smaller := left
	if right < smaller {
		smaller = right
	}
	tail := 0.0
	for k := 0; k <= smaller; k++ {
		tail += math.Exp(logChoose(discordant, k) - float64(discordant)*math.Ln2)
	}
	return math.Min(1, 2*tail)
}

func logChoose(n, k int) float64 {
	top, _ := math.Lgamma(float64(n) + 1)
	left, _ := math.Lgamma(float64(k) + 1)
	right, _ := math.Lgamma(float64(n-k) + 1)
	return top - left - right
}
