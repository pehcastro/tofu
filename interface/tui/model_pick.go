package tui

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"strings"

	"tofu/interface/tui/models"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/internal/judge/jev"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	isettings "tofu/internal/settings"
	isubagent "tofu/internal/subagent"
	"tofu/internal/sys"
	shipped "tofu/library"
)

const (
	pickedHead    = "the next turn runs "
	pickedEffort  = " at effort "
	pickedOnce    = ", and a restart starts again on the bound model"
	noWireToPick  = "no subscription is signed in, so there is no model to pick"
	noWireForPick = " has no signed-in subscription or stored key, so the next turn keeps its model"
	notSet        = "not set"
)

func layeredModels(root string) func() (library.Library, error) {
	return func() (library.Library, error) {
		layers, err := library.Layers(shipped.Files(), root)
		if err != nil {
			return library.Library{}, err
		}
		return library.Load(layers)
	}
}

func (a *App) openPicker(assign string) {
	loaded, err := a.options.Models()
	if err != nil {
		a.notify(err.Error())
		return
	}
	sources := make([]models.Source, 0, len(a.wires))
	for _, wire := range a.wires {
		sources = append(sources, models.Source{ID: library.Subscription(wire.Provider), Efforts: wire.Efforts})
	}
	rows := a.roleRows()
	targets := make([]models.Target, len(rows))
	for index, row := range rows {
		targets[index] = models.Target{Name: row.Label, Job: row.Description, Assigned: row.Value}
		if row.Action == settings.RowRole {
			targets[index].Role = library.RoleID(strings.TrimPrefix(row.Key, roleKeyPrefix))
		}
	}
	if spec, isSetting := a.pickedSetting(assign); isSetting {
		rows = append(rows, settings.Row{Key: assign})
		targets = append(targets, models.Target{Name: spec.Label, Job: spec.Description, Assigned: cmp.Or(a.store.Text(assign), notSet), Setting: assign})
	}
	envFile := filepath.Join(a.options.Root, sys.CredentialFileName)
	keys := models.Keys{
		Set: func(name string) bool { _, err := jev.KeyFor(envFile, name); return err == nil },
		Save: func(ctx context.Context, name, value string) error {
			if name == library.Meta.KeyName() {
				return library.StoreMetaKey(ctx, value)
			}
			return sys.SaveKey(name, value)
		},
	}
	picker := models.Build(loaded, sources, keys, targets)
	if len(picker.Groups) == 0 && assign != roleKeyPrefix+string(library.RoleClassifier) {
		a.notify(noWireToPick)
		return
	}
	picker.SetSize(a.width, a.height)
	if at := slices.IndexFunc(rows, func(row settings.Row) bool { return row.Key == assign }); at >= 0 {
		picker.AssignTo(at)
	}
	a.push(&modelsDialog{picker: picker})
}

func (a *App) pickedSetting(key string) (isettings.Spec, bool) {
	if a.store == nil || key != isettings.BrowserModel {
		return isettings.Spec{}, false
	}
	return a.spec(key)
}

func (a *App) nowRuns(name string) string {
	a.readRoles()
	at := slices.IndexFunc(a.defined, func(definition isubagent.Definition) bool { return definition.Name == name })
	if at < 0 {
		return name + " is no longer found"
	}
	definition := a.defined[at]
	switch definition.Runs {
	case isubagent.RunsModel:
		return name + " now runs " + definition.Model
	case isubagent.RunsInherit:
		return name + " now runs on the orchestrator's model"
	case isubagent.RunsDisabled:
		return name + " is disabled"
	case isubagent.RunsRefused:
		return name + " is refused: " + strings.Join(definition.Refused, "; ")
	}
	panic("tui: unknown sub-agent state " + string(definition.Runs))
}

func (a *App) runNextTurnOn(slug string, effort llm.Effort) {
	source, model, _ := strings.Cut(slug, "/")
	if !slices.ContainsFunc(a.wires, func(wire Wire) bool { return wire.Provider == source }) {
		a.readWires()
	}
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
	a.notify(slug + noWireForPick)
}
