package prompts

import (
	"math"
	"sort"
)

func pearson(xs, ys []float64) float64 {
	if len(xs) != len(ys) || len(xs) < 2 {
		return math.NaN()
	}
	var meanX, meanY float64
	for i := range xs {
		meanX += xs[i]
		meanY += ys[i]
	}
	meanX /= float64(len(xs))
	meanY /= float64(len(ys))
	var product, squaredX, squaredY float64
	for i := range xs {
		dx, dy := xs[i]-meanX, ys[i]-meanY
		product += dx * dy
		squaredX += dx * dx
		squaredY += dy * dy
	}
	if squaredX == 0 || squaredY == 0 {
		return math.NaN()
	}
	return product / math.Sqrt(squaredX*squaredY)
}

func ranks(values []float64) []float64 {
	order := make([]int, len(values))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return values[order[i]] < values[order[j]] })
	ranked := make([]float64, len(values))
	for start := 0; start < len(order); {
		end := start
		for end+1 < len(order) && values[order[end+1]] == values[order[start]] {
			end++
		}
		shared := float64(start+end)/2 + 1
		for _, position := range order[start : end+1] {
			ranked[position] = shared
		}
		start = end + 1
	}
	return ranked
}

func spearman(xs, ys []float64) float64 {
	if len(xs) != len(ys) {
		return math.NaN()
	}
	return pearson(ranks(xs), ranks(ys))
}

type Table struct {
	ThresholdTokens int64
	VolumeAndWrong  int
	VolumeOnly      int
	WrongOnly       int
	Neither         int
	DistinctBoth    int
	Phi             float64
}

func cross(sessions []Session, thresholdTokens int64) Table {
	table := Table{ThresholdTokens: thresholdTokens}
	both := map[string]bool{}
	volume := make([]float64, len(sessions))
	wrong := make([]float64, len(sessions))
	for i, session := range sessions {
		hasVolume := session.ReadTokens >= thresholdTokens
		hasWrong := session.WrongDirections() > 0
		switch {
		case hasVolume && hasWrong:
			table.VolumeAndWrong++
			both[session.Task] = true
		case hasVolume:
			table.VolumeOnly++
		case hasWrong:
			table.WrongOnly++
		default:
			table.Neither++
		}
		if hasVolume {
			volume[i] = 1
		}
		if hasWrong {
			wrong[i] = 1
		}
	}
	table.DistinctBoth = len(both)
	table.Phi = pearson(volume, wrong)
	return table
}
