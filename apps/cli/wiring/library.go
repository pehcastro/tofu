package wiring

import (
	"fmt"
	"slices"
	"strings"

	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/core"
	shipped "tofu/library"
)

func ModelLibrary(dir string) (catalog.Library, error) {
	layers, err := catalog.Layers(shipped.Files(), dir)
	if err != nil {
		return catalog.Library{}, err
	}
	library, _ := catalog.Load(layers)
	return library, nil
}

func OutsideTheLibrary(library catalog.Library, wire string) error {
	return fmt.Errorf(
		"the model library covers the subscriptions reached by --wire %s, and --wire %s spends an api key, which is money rather than a window",
		strings.Join(library.Wires(), " and --wire "), wire)
}

func SelectModel(wire, want string) (catalog.Model, error) {
	library, err := ModelLibrary("")
	if err != nil {
		return catalog.Model{}, err
	}
	spec, carried := library.ForWire(wire)
	if !carried {
		return catalog.Model{}, OutsideTheLibrary(library, wire)
	}
	if want == "" {
		return library.Default(spec.ID)
	}
	model, err := library.Select(want)
	if err != nil {
		return catalog.Model{}, err
	}
	if model.Subscription != spec.ID {
		return catalog.Model{}, fmt.Errorf("%s belongs to the %s subscription, and --wire %s reaches %s",
			model.Slug(), model.Subscription, wire, spec.ID)
	}
	return model, nil
}

func BoundRoles(wire, dir string) (catalog.Bindings, error) {
	library, err := ModelLibrary(dir)
	if err != nil {
		return nil, err
	}
	spec, carried := library.ForWire(wire)
	if !carried {
		return nil, OutsideTheLibrary(library, wire)
	}
	return library.Bind(spec.ID)
}

func DefaultEffort(offered []core.Effort) core.Effort {
	floor := slices.Index(core.Efforts(), core.EffortLow)
	at := slices.IndexFunc(offered, func(effort core.Effort) bool { return slices.Index(core.Efforts(), effort) >= floor })
	return offered[max(at, 0)]
}
