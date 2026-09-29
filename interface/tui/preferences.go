package tui

import (
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/feed"
	"tofu/interface/tui/hostkeys"
	"tofu/interface/tui/settings"
	"tofu/internal/keymap"
	library "tofu/internal/llm/models"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	isubagent "tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	animationsOff     = "off"
	switchOn          = "on"
	switchOff         = "off"
	rolesCategory     = "Models & roles"
	roleKeyPrefix     = "role:"
	agentKeyPrefix    = "agent:"
	orchestratorLabel = "Orchestrator model"
	classifierLabel   = "Classifier model"
	keybindingsKey    = "keybindings"
	hostKey           = "host"
	toolEventKind     = "tool"
	defaultSource     = "default"
	appearanceGroup   = "Appearance"
)

type preview struct{ key, value string }

func (a *App) text(key string) string {
	if a.preview.key == key {
		return a.preview.value
	}
	if a.store == nil {
		return a.declared(key).DefaultText
	}
	return a.store.Text(key)
}

func (a *App) declared(key string) isettings.Spec {
	at := slices.IndexFunc(a.defaults, func(spec isettings.Spec) bool { return spec.Key == key })
	if at < 0 {
		return isettings.Spec{}
	}
	return a.defaults[at]
}

func (a *App) flag(key string) bool {
	if a.preview.key == key {
		return a.preview.value == switchOn
	}
	if a.store == nil {
		return a.declared(key).Default != 0
	}
	return a.store.Bool(key)
}

func onOff(on bool) string {
	if on {
		return switchOn
	}
	return switchOff
}

func (a *App) settingsKey(key string) tea.Cmd {
	return a.applyIntent(a.settings.Key(key))
}

func (a *App) applyIntent(intent settings.Intent) tea.Cmd {
	var cmd tea.Cmd
	switch intent.Action {
	case settings.ActionNone:
	case settings.ActionPreview:
		a.preview = preview{intent.Key, intent.Value}
	case settings.ActionCommit:
		a.preview = preview{}
		a.commit(intent.Key, intent.Value)
	case settings.ActionRevert:
		a.preview = preview{}
	case settings.ActionToggle:
		a.commit(intent.Key, onOff(!a.flag(intent.Key)))
	case settings.ActionIncrement:
		a.setNumber(intent.Key, 1)
	case settings.ActionDecrement:
		a.setNumber(intent.Key, -1)
	case settings.ActionCycleScope:
		a.settings.Scope = (a.settings.Scope + 1) % len(a.settings.Scopes)
	case settings.ActionOpenKeybindings:
		cmd = a.push(&shortcutsDialog{hostkeys.NewShortcuts(a.options.Keymap, a.shortcuts)})
	case settings.ActionOpenHost:
		cmd = a.push(&hostDialog{hostkeys.NewHost(a.shortcuts)})
	case settings.ActionOpenRole:
		a.openPicker(intent.Key)
	case settings.ActionClose:
		cmd = a.show(screenChat)
	default:
		panic("tui: unknown settings action")
	}
	a.refreshSettingsRows()
	return cmd
}

func (a *App) setNumber(key string, by int) {
	if a.store == nil {
		return
	}
	spec, known := a.spec(key)
	if !known {
		return
	}
	if err := a.store.Set(isettings.Scope(a.settings.Scope), key, min(max(a.store.Int(key)+by, spec.Least), spec.Most)); err != nil {
		a.notify(err.Error())
	}
}

func (a *App) commit(key, value string) {
	if a.store == nil {
		return
	}
	scope := isettings.Scope(a.settings.Scope)
	spec, known := a.spec(key)
	if !known {
		return
	}
	var err error
	switch spec.Kind {
	case isettings.Bool:
		wasOn, on := a.store.Bool(key), value == switchOn
		number := 0
		if on {
			number = 1
		}
		err = a.store.Set(scope, key, number)
		if key == isettings.ChatShowsTools && err == nil && wasOn != on {
			a.recordKindMove(wasOn)
		}
	case isettings.Text:
		err = a.store.SetText(scope, key, value)
	case isettings.Int:
		number, parsed := strconv.Atoi(value)
		if parsed != nil {
			return
		}
		err = a.store.Set(scope, key, number)
	default:
		panic("tui: unknown setting kind")
	}
	if err != nil {
		a.notify(err.Error())
	}
}

func (a *App) spec(key string) (isettings.Spec, bool) {
	table := a.store.Table()
	at := slices.IndexFunc(table, func(spec isettings.Spec) bool { return spec.Key == key })
	if at < 0 {
		return isettings.Spec{}, false
	}
	return table[at], true
}

func (a *App) recordKindMove(wasInChat bool) {
	row := isession.Promotion{
		Action:    isession.MovedKind,
		EventKind: toolEventKind,
		FreeArm:   isession.PlaceWork,
		Chose:     isession.PlaceChat,
	}
	if wasInChat {
		row.FreeArm, row.Chose = isession.PlaceChat, isession.PlaceWork
	}
	a.appendPromotion(row)
}

func (a *App) recordReach(id string) {
	at := a.happenedAt(id)
	if at < 0 || slices.Contains(a.reached, id) {
		return
	}
	event := a.happened[at]
	if event.Kind != feed.KindTool && event.Kind != feed.KindEdit {
		return
	}
	a.reached = append(a.reached, id)
	freeArm := isession.PlaceChat
	if !a.flag(isettings.ChatShowsTools) {
		freeArm = isession.PlaceWork
	}
	a.appendPromotion(isession.Promotion{
		Action:    isession.ReachedIntoWork,
		EventID:   id,
		EventKind: event.Title,
		FreeArm:   freeArm,
		Chose:     isession.PlaceChat,
	})
}

func (a *App) appendPromotion(row isession.Promotion) {
	row.At, row.Session = a.options.Now(), a.sessionID
	if err := a.options.Promotions.Append(row); err != nil {
		a.notify(err.Error())
	}
}

func (a *App) refreshSettingsRows() {
	if a.store == nil {
		return
	}
	pending := a.store.RestartPending()
	var rows []settings.Row
	rolesPlaced := false
	for _, spec := range a.store.Table() {
		if !rolesPlaced && spec.Category != appearanceGroup {
			rows, rolesPlaced = append(rows, a.roleRows()...), true
		}
		rows = append(rows, a.settingRow(spec, slices.Contains(pending, spec.Key)))
		if spec.Key == isettings.Composer {
			rows = append(rows,
				settings.Row{Key: keybindingsKey, Category: spec.Category, Label: "Keybindings", Description: "Change shortcuts handled inside tofu", Action: settings.RowKeybindings},
				settings.Row{Key: hostKey, Category: spec.Category, Label: "Host integration", Description: "Detect host shortcut conflicts and available integrations", Value: string(keymap.DetectHost().Name), Action: settings.RowHostIntegration})
		}
	}
	a.settings.SetRows(rows)
	a.settings.ChatShowsTools = a.flag(isettings.ChatShowsTools)
	a.settings.SetDensity(a.text(isettings.Density))
}

func (a *App) readRoles() {
	if a.roles != nil {
		return
	}
	a.roles = map[library.RoleID]string{}
	if loaded, err := a.options.Models(); err == nil {
		for _, role := range loaded.Roles {
			a.roles[role.ID] = role.Model.Slug()
		}
		stored, _ := sys.StoredKeys()
		if classifier, err := loaded.Classifier(stored); err == nil {
			a.roles[library.RoleClassifier] = classifier.Slug()
		}
	}
	a.defined = nil
	if a.options.Agents != nil {
		a.defined = a.options.Agents().Definitions
	}
}

func runsOn(definition isubagent.Definition) string {
	if definition.Runs == isubagent.RunsModel {
		return definition.Model
	}
	return string(definition.Runs)
}

func (a *App) roleRow(role library.RoleID, label string) settings.Row {
	return settings.Row{Key: roleKeyPrefix + string(role), Category: rolesCategory, Label: label, Description: role.What(), Value: a.roles[role], Action: settings.RowRole}
}

func (a *App) roleRows() []settings.Row {
	a.readRoles()
	rows := []settings.Row{a.roleRow(library.RoleOrchestrator, orchestratorLabel), a.roleRow(library.RoleClassifier, classifierLabel)}
	for _, definition := range a.defined {
		rows = append(rows, settings.Row{Key: agentKeyPrefix + definition.Name, Category: rolesCategory, Label: definition.Name, Origin: definition.Origin,
			Description: definition.Description, Value: runsOn(definition), Source: definition.AssignedIn, Path: definition.Path, Action: settings.RowSubAgent})
	}
	return rows
}

func (a *App) settingRow(spec isettings.Spec, pending bool) settings.Row {
	scope, fromFile := a.store.Source(spec.Key)
	source := defaultSource
	if fromFile {
		source = scope.String() + " " + a.store.Path(scope)
	}
	row := settings.Row{
		Key:             spec.Key,
		Category:        spec.Category,
		Label:           spec.Label,
		Description:     spec.Description,
		Kind:            settings.Text,
		Value:           a.store.Text(spec.Key),
		Choices:         spec.Choices,
		Changed:         fromFile,
		RestartRequired: spec.Restart,
		RestartPending:  pending,
		Source:          source,
	}
	if _, picked := a.pickedSetting(spec.Key); picked {
		row.Action = settings.RowModel
	}
	switch spec.Kind {
	case isettings.Bool:
		row.Value, row.Choices = onOff(a.store.Bool(spec.Key)), []string{switchOff, switchOn}
	case isettings.Int:
		row.Kind, row.Value, row.Number = settings.Int, strconv.Itoa(a.store.Int(spec.Key)), numberRange(spec)
	case isettings.Text:
	default:
		panic("tui: unknown setting kind")
	}
	return row
}

func numberRange(spec isettings.Spec) settings.Number {
	return settings.Number{Least: spec.Least, Most: spec.Most, DefaultMeaning: strconv.Itoa(spec.Default) + " " + spec.Unit}
}
