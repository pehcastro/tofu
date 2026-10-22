package drain

import (
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
	byModel := map[string]Weights{}
	for slug, price := range registry.Prices {
		if price.Input <= 0 {
			continue
		}
		weights := defaultWeights()
		weights.Output, weights.From = price.Output/price.Input, price.From
		if price.CacheRead > 0 {
			weights.Read = price.CacheRead / price.Input
		}
		if price.CacheWrite > 0 {
			weights.Write5m = price.CacheWrite / price.Input
		}
		byModel[slug], byModel[modelID(slug)] = weights, weights
	}
	return func(model string) Weights {
		if weights, priced := byModel[model]; priced {
			return weights
		}
		if weights, priced := byModel[modelID(model)]; priced {
			return weights
		}
		return defaultWeights()
	}
}

func modelID(slug string) string { return slug[strings.LastIndex(slug, "/")+1:] }
