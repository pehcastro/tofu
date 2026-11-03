package thrift

import "sort"

type Spread struct {
	Count  int
	Zeros  int
	Sum    int64
	Min    int64
	P25    int64
	Median int64
	P75    int64
	P90    int64
	Worst  int64
}

func Measure(values []int64) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	spread := Spread{Count: len(sorted), Min: sorted[0], Worst: sorted[len(sorted)-1]}
	for _, value := range sorted {
		spread.Sum += value
		if value == 0 {
			spread.Zeros++
		}
	}
	spread.P25 = percentile(sorted, 0.25)
	spread.Median = percentile(sorted, 0.50)
	spread.P75 = percentile(sorted, 0.75)
	spread.P90 = percentile(sorted, 0.90)
	return spread
}

func percentile(sorted []int64, fraction float64) int64 {
	index := int(fraction * float64(len(sorted)-1))
	return sorted[index]
}

type Bucket struct {
	Low   int64
	High  int64
	Count int
}

func Histogram(values []int64) []Bucket {
	edges := []int64{0, 1, 3, 6, 11, 21, 51, 101, 251, 501}
	buckets := make([]Bucket, len(edges))
	for i, low := range edges {
		high := int64(-1)
		if i+1 < len(edges) {
			high = edges[i+1] - 1
		}
		buckets[i] = Bucket{Low: low, High: high}
	}
	for _, value := range values {
		slot := 0
		for i, low := range edges {
			if value >= low {
				slot = i
			}
		}
		buckets[slot].Count++
	}
	return buckets
}
