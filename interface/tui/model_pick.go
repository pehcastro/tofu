package tui

import (
	"strings"

	"tofu/interface/tui/models"
	"tofu/interface/tui/session"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	shipped "tofu/library"
)

const (
	pickedHead   = "the next turn runs "
	pickedEffort = " at effort "
	pickedOnce   = ", and a restart starts again on the bound model"
	noWireToPick = "no subscription is signed in, so there is no model to pick"
)

func shippedModels() (library.Library, error) {
	layers, err := library.Layers(shipped.Files(), "")
	if err != nil {
		return library.Library{}, err
	}
	return library.Load(layers)
}

func (a *App) openPicker() {
	if len(a.wires) == 0 {
		a.view.Append(session.Entry{Kind: session.Note, Body: noWireToPick})
		return
	}
	loaded, err := a.options.Models()
	if err != nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: err.Error()})
		return
	}
	sources := make([]models.Source, 0, len(a.wires))
	for _, wire := range a.wires {
		sources = append(sources, models.Source{ID: library.Subscription(wire.Provider), Efforts: wire.Efforts})
	}
	a.picker = models.Build(loaded, sources)
	a.picker.SetSize(a.width, a.height-viewChrome)
	a.show(viewModels)
}

func (a *App) pickerKey(key string) {
	if key != "enter" {
		a.picker.Key(key)
		return
	}
	row, picked := a.picker.Picked()
	a.show(viewChat)
	if !picked || row.Use == library.UseExcluded {
		return
	}
	a.runNextTurnOn(row.Slug, a.picker.Effort())
}

func (a *App) runNextTurnOn(slug string, effort llm.Effort) {
	source, model, _ := strings.Cut(slug, "/")
	for _, wire := range a.wires {
		if wire.Provider != source {
			continue
		}
		a.wire, a.provider, a.model = wire.Name, wire.Provider, model
		a.picked, a.effort = slug, effort
		note := pickedHead + slug
		if effort != "" {
			note += pickedEffort + string(effort)
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: note + pickedOnce})
		return
	}
}
