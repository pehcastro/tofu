package wiring

import (
	"strings"

	"tofu/internal/models/llm/catalog"
	"tofu/internal/store/session/drain"
)

func PricedByProvider(registry catalog.Registry) (func(slug string) drain.Weights, error) {
	library, err := ModelLibrary("")
	weights := drain.WeightsFrom(registry)
	return func(slug string) drain.Weights {
		wire, model, wired := strings.Cut(slug, "/")
		if spec, subscribed := library.ForWire(wire); wired && subscribed {
			slug = string(spec.Provider) + "/" + model
		}
		return weights(slug)
	}, err
}
