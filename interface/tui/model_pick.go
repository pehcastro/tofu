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

func (a *App) openPicker(onRoles bool) {
	if len(a.wires) == 0 {
		a.notify(noWireToPick)
		return
	}
	loaded, err := a.options.Models()
	if err != nil {
		a.notify(err.Error())
		return
	}
	sources := make([]models.Source, 0, len(a.wires))
	for _, wire := range a.wires {
		sources = append(sources, models.Source{ID: library.Subscription(wire.Provider), Efforts: wire.Efforts})
	}
	picker := models.Build(loaded, sources)
	picker.SetSize(a.width, a.height)
	if onRoles {
		picker.Key("tab")
	}
	a.push(&modelsDialog{picker})
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
