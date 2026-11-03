package drain

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"tofu/internal/llm/models"
)

const (
	inputWeight   = 1
	write5mWeight = 1.25
	write1hWeight = 2
	readWeight    = 0.1
	outputWeight  = 5
)

type Weights struct {
	Input   float64 `json:"input"`
	Write5m float64 `json:"cache_write_5m"`
	Write1h float64 `json:"cache_write_1h"`
	Read    float64 `json:"cache_read"`
	Output  float64 `json:"output"`
	From    string  `json:"from"`
}

func defaultWeights() Weights {
	return Weights{Input: inputWeight, Write5m: write5mWeight, Write1h: write1hWeight, Read: readWeight, Output: outputWeight, From: "list price ratios"}
}

func WeightsFrom(registry models.Registry) func(model string) Weights {
	bySlug := map[string]Weights{}
	for slug, price := range registry.Prices {
		if price.Input <= 0 {
			continue
		}
		weights := defaultWeights()
		weights.Output, weights.From = price.Output/price.Input, slug+", "+price.From
		if price.CacheRead > 0 {
			weights.Read = price.CacheRead / price.Input
		}
		if price.CacheWrite > 0 {
			weights.Write5m = price.CacheWrite / price.Input
		}
		bySlug[slug] = weights
	}
	priced := bySource(bySlug)
	return func(model string) Weights { return cmp.Or(priced(model), defaultWeights()) }
}

func WindowsFrom(registry models.Registry) func(model string) int {
	return bySource(registry.Windows)
}

func bySource[V any](bySlug map[string]V) func(model string) V {
	byModel := map[string]V{}
	for _, slug := range slices.Sorted(maps.Keys(bySlug)) {
		byModel[slug] = bySlug[slug]
		if _, taken := byModel[modelID(slug)]; !taken {
			byModel[modelID(slug)] = bySlug[slug]
		}
	}
	return func(model string) V {
		if value, found := byModel[model]; found {
			return value
		}
		return byModel[modelID(model)]
	}
}

func modelID(slug string) string { return slug[strings.LastIndex(slug, "/")+1:] }
