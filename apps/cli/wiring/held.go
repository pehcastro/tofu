package wiring

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	cli "tofu/apps/cli/page"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/project"
	"tofu/internal/ui/tui/app"
	"tofu/internal/ui/tui/pickers/settings"
)

const (
	GateOffNote      = "gate off: no tool call is judged until tofu login classifier stores the key of the bound classifier"
	NoCredential     = "no subscription is signed in, so no model can answer"
	ModelStep        = "language model"
	ClassifierStep   = "classifier · jev, required"
	UnreadableSource = "unreadable: "
	JevName          = "jev"
)

func OpenStoredCredentials() (*accounts.Store, error) {
	path, err := accounts.Path()
	if err != nil {
		return nil, err
	}
	present, err := fs.Exists(path)
	if err != nil || !present {
		return nil, err
	}
	return accounts.Open(path, Specs())
}

type HeldStore struct {
	Store *accounts.Store
	Path  string
	Err   error
}

func HoldStore() HeldStore {
	path, err := accounts.Path()
	if err != nil {
		return HeldStore{Err: err}
	}
	store, err := accounts.Open(path, Specs())
	return HeldStore{Store: store, Path: path, Err: err}
}

func (h HeldStore) Close() {
	if h.Store != nil {
		_ = h.Store.Close()
	}
}

func (h HeldStore) Keys() core.Keyring {
	if h.Err != nil {
		return core.ReadKeyring()
	}
	stored, err := h.Store.Keys()
	return core.Keyring{Stored: stored, Path: h.Path, ReadErr: err}
}

func LoginCommand(provider string) func() *exec.Cmd {
	return func() *exec.Cmd {
		return outside.Command(context.Background(), os.Args[0], append([]string{"login"}, LoginWords(provider)...)...).Cmd
	}
}

func (h HeldStore) Wires() []app.Wire {
	library, err := ModelLibrary("")
	if h.Err != nil || err != nil {
		return nil
	}
	now := time.Now()
	var wires []app.Wire
	for _, spec := range library.Subscriptions {
		row, present, err := h.Store.RowAt(accounts.Provider(spec.ID), now)
		if err != nil || !present || row.Unusable(now) != "" {
			continue
		}
		if selected, err := library.Default(spec.ID); err == nil {
			wires = append(wires, app.Wire{Name: spec.Wire, Model: selected.ID, Provider: string(spec.ID), Efforts: WireEfforts(spec.Wire)})
		}
	}
	if _, err := h.Keys().KeyFor(credential.CredentialFileName, catalog.Meta.KeyName()); err != nil {
		return wires
	}
	if chosen, err := library.Default(catalog.Subscription(catalog.Meta)); err == nil {
		wires = append(wires, app.Wire{Name: WireMeta, Model: chosen.ID, Provider: string(catalog.Meta), Efforts: chosen.Efforts})
	}
	return wires
}

type StartStep struct {
	Label   string
	Step    string
	What    string
	Command string
	Done    string
	Choices []app.Choice
}

func (h HeldStore) StartSteps() []StartStep {
	return []StartStep{h.OrchestratorStep(), ClassifierStepOf(h.Keys())}
}

func (h HeldStore) OrchestratorStep() StartStep {
	step := StartStep{Label: string(catalog.RoleOrchestrator), Step: ModelStep}
	library, _ := ModelLibrary(".")
	orchestrator, bound := library.Bound(catalog.RoleOrchestrator)
	wires := h.Wires()
	signed := slices.ContainsFunc(wires, func(wire app.Wire) bool { return wire.Provider == orchestrator.Source() })
	switch {
	case bound && signed:
		step.Done = orchestrator.Source() + " · " + orchestrator.ID
		return step
	case bound:
		step.What, step.Command = "the orchestrator runs "+orchestrator.Slug()+", and "+orchestrator.Source()+" is not signed in", LoginHint(orchestrator.Source())
	case len(wires) == 0:
		step.What, step.Command = NoCredential, CommandFor("login", host.RoleLLM, "<source>")
	default:
		step.What, step.Command = catalog.RoleOrchestrator.Unset().Error(), "tofu models bind orchestrator <provider/model>"
	}
	for _, wire := range wires {
		slug := wire.Provider + "/" + wire.Model
		step.Choices = append(step.Choices, app.Choice{Label: "bind " + slug, Run: func() *exec.Cmd {
			return outside.Command(context.Background(), os.Args[0], "models", "bind", string(catalog.RoleOrchestrator), slug).Cmd
		}})
	}
	for _, spec := range library.Subscriptions {
		if !slices.ContainsFunc(wires, func(wire app.Wire) bool { return wire.Provider == string(spec.ID) }) {
			step.Choices = append(step.Choices, app.Choice{Label: string(spec.ID) + ", signs in through the browser", Run: LoginCommand(string(spec.ID))})
		}
	}
	if !slices.ContainsFunc(wires, func(wire app.Wire) bool { return wire.Name == WireMeta }) {
		step.Choices = append(step.Choices, app.Choice{Label: catalog.Meta.Display() + " API key", Key: credential.MetaMuseKeyName})
	}
	return step
}

func ClassifierStepOf(keys core.Keyring) StartStep {
	step := StartStep{Label: JevName, Step: ClassifierStep}
	route, err := classifiers.Resolve(".", keys)
	if core.IsUnbound(err) {
		step.What, step.Command = err.Error(), "tofu models bind classifier <provider/model>"
		library, _ := ModelLibrary(".")
		for _, model := range library.Models {
			if model.Kind == catalog.KindClassifier && model.Use != catalog.UseExcluded {
				step.Choices = append(step.Choices, app.Choice{Label: "bind " + model.Slug(), Run: func() *exec.Cmd {
					return outside.Command(context.Background(), os.Args[0], "models", "bind", string(catalog.RoleClassifier), model.Slug()).Cmd
				}})
			}
		}
		return step
	}
	provider := route.Model.Provider
	if provider == "" {
		step.What = err.Error()
		return step
	}
	step.What = "there is no " + string(provider) + " key, so jev judges no tool call"
	step.Command = CommandFor("login", host.RoleClassifier, string(provider))
	step.Choices = []app.Choice{{Label: provider.Display() + " key", Key: classifiers.KeyName(provider)}}
	if err == nil {
		step.Done = provider.Display() + " key " + render.Mask(route.Key)
	}
	return step
}

func GateOffSaid(err error) string {
	if core.IsUnbound(err) {
		return err.Error()
	}
	return GateOffNote
}

func (h HeldStore) StartBlockers() []StartStep {
	return slices.DeleteFunc(h.StartSteps(), func(step StartStep) bool { return step.Done != "" })
}

func (h HeldStore) Requirements() []app.Requirement {
	steps := h.StartSteps()
	needed := make([]app.Requirement, 0, len(steps))
	for _, step := range steps {
		needed = append(needed, app.Requirement{Step: step.Step, What: step.What, Fix: step.Command, Done: step.Done, Choices: step.Choices})
	}
	return needed
}

func (h HeldStore) Providers() []settings.Provider {
	library, _ := ModelLibrary("")
	var rows []settings.Provider
	for _, spec := range library.Subscriptions {
		rows = append(rows, h.SubscriptionProvider(accounts.Provider(spec.ID)))
	}
	return append(rows, OpenRouterProvider(h.Keys()), JevProvider(h.Keys()))
}

func (h HeldStore) SubscriptionProvider(provider accounts.Provider) settings.Provider {
	row := settings.Provider{Name: string(provider), Source: h.Path}
	if h.Err != nil {
		row.Fix = UnreadableSource + h.Err.Error()
		return row
	}
	now := time.Now()
	stored, present, err := h.Store.RowAt(provider, now)
	switch {
	case err != nil:
		row.Fix = UnreadableSource + err.Error()
	case !present:
		row.Fix = "run " + LoginHint(string(provider))
	case stored.Unusable(now) != "":
		row.State = stored.Unusable(now)
	default:
		row.State = strings.TrimSpace("signed in " + MaskedAccount(stored.Credential.Identity))
	}
	return row
}

func OpenRouterProvider(keys core.Keyring) settings.Provider {
	located, err := keys.LocateFor(credential.CredentialFileName, core.OpenRouterVariable)
	row := settings.Provider{Name: OpenRouterName, Source: located.Path}
	if located.Source == core.SourceEnvironment {
		row.Source = located.Name + " in the environment"
	}
	key, keyErr := keys.KeyFor(credential.CredentialFileName, core.OpenRouterVariable)
	if err != nil || keyErr != nil {
		row.Fix = OpenRouterFix
		return row
	}
	row.Key = render.Mask(key)
	return row
}

func JevProvider(keys core.Keyring) settings.Provider {
	dir, err := project.LogDir()
	if err != nil {
		return settings.Provider{Name: JevName, Fix: UnreadableSource + err.Error()}
	}
	row := settings.Provider{Name: JevName, Source: dir}
	key, keyErr := GateKeyFrom(keys)
	if keyErr == nil {
		row.Key = render.Mask(key)
	}
	current, err := CurrentBuild(dir, "")
	switch {
	case err != nil:
		row.Fix = UnreadableSource + err.Error()
	case keyErr != nil:
		row.Fix = GateOffSaid(keyErr)
	case current.Known:
		row.State = current.Build
	default:
		row.Fix = "no decision recorded yet"
	}
	return row
}

func DoctorBlockers() []host.DoctorBlocker {
	held := HoldStore()
	defer held.Close()
	var blockers []host.DoctorBlocker
	for _, blocker := range held.StartBlockers() {
		blockers = append(blockers, host.DoctorBlocker{Label: blocker.Label, What: blocker.What, Command: blocker.Command})
	}
	return blockers
}

func BlockerRows(page cli.Page, blockers []host.DoctorBlocker) []string {
	rows := make([]cli.Row, len(blockers))
	for i, blocker := range blockers {
		rows[i] = cli.Row{Mark: cli.Fail, Cells: []string{blocker.Label, blocker.What}}
	}
	var lines []string
	for i, line := range page.Rows(rows) {
		lines = append(lines, cli.Indent(line, cli.Gap+page.Hint(blockers[i].Command))...)
	}
	return lines
}
