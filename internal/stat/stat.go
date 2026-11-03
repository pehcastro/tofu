package stat

import "sort"

func Median(values []float64) float64 {
	return Percentile(values, 50)
}

func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p / 100 * float64(len(sorted)-1)
	low := int(rank)
	high := low + 1
	if high >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(low)
	return sorted[low] + frac*(sorted[high]-sorted[low])
}

func Spread(values []float64) (min, max float64) {
	if len(values) == 0 {
		return 0, 0
	}
	min, max = values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}
