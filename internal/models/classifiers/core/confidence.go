package jev

import (
	"math"
	"strconv"
)

func choiceConfidence(probabilities map[string]float64) float64 {
	if len(probabilities) <= 1 {
		return 1
	}
	total, peak := 0.0, 0.0
	for _, probability := range probabilities {
		total += probability
		if probability > peak {
			peak = probability
		}
	}
	if total == 0 {
		return 0
	}
	uniform := 1 / float64(len(probabilities))
	return (peak/total - uniform) / (1 - uniform)
}

func scoreConfidence(probabilities map[string]float64) float64 {
	levels := len(probabilities)
	if levels <= 1 {
		return 1
	}
	shares := make([]float64, levels)
	total := 0.0
	for key, probability := range probabilities {
		level, err := strconv.Atoi(key)
		if err != nil || level < 0 || level >= levels {
			return 0
		}
		shares[level] = probability
		total += probability
	}
	if total == 0 {
		return 0
	}

	mode := 0
	for level, share := range shares {
		if share > shares[mode] {
			mode = level
		}
	}
	spread := 0.0
	for level, share := range shares {
		spread += share / total * math.Abs(float64(level-mode))
	}
	center := float64(levels-1) / 2
	uniformSpread := 0.0
	for level := range shares {
		uniformSpread += math.Abs(float64(level) - center)
	}
	uniformSpread /= float64(levels)
	return math.Max(0, 1-spread/uniformSpread)
}
