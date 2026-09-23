package tui

import (
	"strings"

	"tofu/interface/tui/models"
	"tofu/interface/tui/session"
	library "tofu/internal/llm/models"
	shipped "tofu/library"
)

const (
	pickedHead     = "the next turn runs "
	pickedWireOnly = ", and a model chosen inside a subscription does not reach the turn yet"
	noWireToPick   = "no subscription is signed in, so there is no model to pick"
)

func shippedModels() (library.Library, error) {
	layers, err := library.Layers(shipped.Files())
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
	sources := make([]library.Subscription, 0, len(a.wires))
	for _, wire := range a.wires {
		sources = append(sources, library.Subscription(wire.Provider))
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
	if !picked {
		return
	}
	a.runNextTurnOn(row.Slug)
}

func (a *App) runNextTurnOn(slug string) {
	source, _, _ := strings.Cut(slug, "/")
	for _, wire := range a.wires {
		if wire.Provider != source {
			continue
		}
		a.wire, a.model, a.provider = wire.Name, wire.Model, wire.Provider
		runs := wire.Provider + "/" + wire.Model
		note := pickedHead + runs
		if runs != slug {
			note += pickedWireOnly
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: note})
		return
	}
}
