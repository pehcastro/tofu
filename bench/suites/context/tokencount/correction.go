package tokencount

import "sort"

type Factor struct {
	Shape         Shape
	N             int
	BytesPerToken float64
	Before        Stats
	After         Stats
}

func FitFactors(samples []Sample) []Factor {
	accountable := Filter(samples, func(s Sample) bool { return s.Accountable })
	shapes := distinctShapes(accountable)
	factors := make([]Factor, 0, len(shapes))
	for _, shape := range shapes {
		group := shaped(accountable, shape)
		bpt := medianBytesPerToken(group)
		corrected := correct(group, bpt)
		factors = append(factors, Factor{
			Shape:         shape,
			N:             len(group),
			BytesPerToken: bpt,
			Before:        statsOf(string(shape)+" before correction", group),
			After:         statsOf(string(shape)+" after correction", corrected),
		})
	}
	return factors
}

func CorrectedAccountable(samples []Sample, factors []Factor) []Sample {
	bptByShape := make(map[Shape]float64, len(factors))
	for _, f := range factors {
		bptByShape[f.Shape] = f.BytesPerToken
	}
	accountable := Filter(samples, func(s Sample) bool { return s.Accountable })
	var out []Sample
	for _, shape := range distinctShapes(accountable) {
		out = append(out, correct(shaped(accountable, shape), bptByShape[shape])...)
	}
	return out
}

func distinctShapes(samples []Sample) []Shape {
	seen := map[Shape]bool{}
	var shapes []Shape
	for _, s := range samples {
		if !seen[s.Shape] {
			seen[s.Shape] = true
			shapes = append(shapes, s.Shape)
		}
	}
	sort.Slice(shapes, func(i, j int) bool { return shapes[i] < shapes[j] })
	return shapes
}

func medianBytesPerToken(samples []Sample) float64 {
	if len(samples) == 0 {
		return 0
	}
	ratios := make([]float64, len(samples))
	for i, s := range samples {
		ratios[i] = float64(s.Bytes) / float64(s.Actual)
	}
	sort.Float64s(ratios)
	return median(ratios)
}

func correct(samples []Sample, bytesPerToken float64) []Sample {
	if bytesPerToken == 0 {
		return samples
	}
	out := make([]Sample, len(samples))
	for i, s := range samples {
		estimate := int(float64(s.Bytes)/bytesPerToken + 0.5)
		out[i] = s
		out[i].Estimate = estimate
		out[i].ErrorPct = errorPct(estimate, s.Actual)
	}
	return out
}

func errorPct(estimate, actual int) float64 {
	if actual == 0 {
		return 0
	}
	diff := estimate - actual
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) / float64(actual) * 100
}
