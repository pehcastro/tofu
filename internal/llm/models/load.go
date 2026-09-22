package models

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"tofu/internal/sys"
)

const (
	modelsDir        = "models"
	subscriptionsDir = "subscriptions"
)

type Layer struct {
	Name   string
	Origin string
	FS     fs.FS
}

func Layers(shipped fs.FS) ([]Layer, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	project, err := sys.ProjectStateDir()
	if err != nil {
		return nil, err
	}
	return []Layer{
		{Name: "library", Origin: "library", FS: shipped},
		{Name: "global", Origin: home, FS: os.DirFS(home)},
		{Name: "project", Origin: project, FS: os.DirFS(project)},
	}, nil
}

type Broken struct {
	File  string
	Field string
	Why   string
}

func (b Broken) Error() string {
	if b.Field == "" {
		return b.File + ": " + b.Why
	}
	return b.File + ": " + b.Field + ": " + b.Why
}

type BrokenLibrary struct{ Refused []Broken }

func (b *BrokenLibrary) Error() string {
	said := make([]string, 0, len(b.Refused))
	for _, one := range b.Refused {
		said = append(said, one.Error())
	}
	return fmt.Sprintf("the library refuses %d of its files: %s", len(said), strings.Join(said, "; "))
}

type sheet struct {
	values map[string]string
	file   string
}

type merged struct {
	order  []string
	sheets map[string]*sheet
}

func newMerged() *merged { return &merged{sheets: map[string]*sheet{}} }

func (m *merged) take(key, file, body string, allowed map[string]bool) *Broken {
	values, bad := fieldsOf(file, body, allowed)
	if bad != nil {
		return bad
	}
	if m.sheets[key] == nil {
		m.sheets[key] = &sheet{values: map[string]string{}}
		m.order = append(m.order, key)
	}
	into := m.sheets[key]
	into.file = file
	for field, value := range values {
		into.values[field] = value
	}
	return nil
}

func fieldsOf(file, body string, allowed map[string]bool) (map[string]string, *Broken) {
	values := map[string]string{}
	for i, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		at := strings.IndexByte(line, ':')
		if at < 0 {
			return nil, &Broken{File: file, Why: fmt.Sprintf("line %d expects key: value, found %q", i+1, line)}
		}
		key := strings.TrimSpace(line[:at])
		if !allowed[key] {
			return nil, &Broken{File: file, Field: key, Why: "unknown field"}
		}
		values[key] = strings.Trim(strings.TrimSpace(line[at+1:]), `"`)
	}
	return values, nil
}

type Contract struct {
	Kind     string
	Required []string
	Optional []string
}

func Contracts() []Contract {
	return []Contract{
		{Kind: modelsDir, Required: []string{"use"}, Optional: []string{"subscription", "reason", "window"}},
		{Kind: subscriptionsDir, Required: []string{"provider", "wire", "windows"}, Optional: []string{"not_models"}},
		{Kind: rolesDir, Required: []string{"model"}},
	}
}

func allowedFields(kind string) map[string]bool {
	for _, contract := range Contracts() {
		if contract.Kind != kind {
			continue
		}
		allowed := make(map[string]bool, len(contract.Required)+len(contract.Optional))
		for _, field := range append(append([]string{}, contract.Required...), contract.Optional...) {
			allowed[field] = true
		}
		return allowed
	}
	panic("models: no contract for " + kind)
}

func Load(layers []Layer) (Library, error) {
	var refused []Broken
	subscriptions, models, roles := newMerged(), newMerged(), newMerged()
	subscriptionFields := allowedFields(subscriptionsDir)
	modelFields := allowedFields(modelsDir)
	roleFields := allowedFields(rolesDir)
	for _, layer := range layers {
		refused = append(refused, readFlat(layer, subscriptionsDir, "a subscription is one yaml file named after itself", subscriptionFields, subscriptions)...)
		refused = append(refused, readModels(layer, modelFields, models)...)
		refused = append(refused, readFlat(layer, rolesDir, "a role is one yaml file named after the role", roleFields, roles)...)
	}

	library := Library{}
	known := map[Subscription]SubscriptionSpec{}
	for _, id := range subscriptions.order {
		spec, bad := buildSubscription(id, subscriptions.sheets[id])
		if bad != nil {
			refused = append(refused, *bad)
			continue
		}
		known[spec.ID] = spec
		library.Subscriptions = append(library.Subscriptions, spec)
	}
	for _, slug := range models.order {
		model, bad := buildModel(slug, models.sheets[slug], known)
		if bad != nil {
			refused = append(refused, *bad)
			continue
		}
		library.Models = append(library.Models, model)
	}
	sort.Slice(library.Subscriptions, func(i, j int) bool {
		return library.Subscriptions[i].ID < library.Subscriptions[j].ID
	})
	sort.Slice(library.Models, func(i, j int) bool {
		return library.Models[i].Slug() < library.Models[j].Slug()
	})
	refused = append(refused, library.defaults()...)
	for _, name := range roles.order {
		role, bad := buildRole(name, roles.sheets[name], library)
		if bad != nil {
			refused = append(refused, *bad)
			continue
		}
		library.Roles = append(library.Roles, role)
	}
	sort.Slice(library.Roles, func(i, j int) bool { return library.Roles[i].ID < library.Roles[j].ID })
	library.Broken = refused
	if len(refused) == 0 {
		return library, nil
	}
	return library, &BrokenLibrary{Refused: refused}
}

func readFlat(layer Layer, dir, why string, allowed map[string]bool, into *merged) []Broken {
	var refused []Broken
	for _, entry := range entriesOf(layer.FS, dir) {
		file := dir + "/" + entry.Name()
		if bad := notAYAMLFile(entry, file, why); bad != nil {
			refused = append(refused, *bad)
			continue
		}
		body, err := fs.ReadFile(layer.FS, file)
		if err != nil {
			refused = append(refused, Broken{File: file, Why: err.Error()})
			continue
		}
		at := sys.Join(layer.Origin, dir, entry.Name())
		if bad := into.take(strings.TrimSuffix(entry.Name(), ".yaml"), at, string(body), allowed); bad != nil {
			refused = append(refused, *bad)
		}
	}
	return refused
}

func readModels(layer Layer, allowed map[string]bool, into *merged) []Broken {
	var refused []Broken
	for _, provider := range entriesOf(layer.FS, modelsDir) {
		if !provider.IsDir() {
			refused = append(refused, Broken{
				File: modelsDir + "/" + provider.Name(),
				Why:  "library/models holds one directory per provider and nothing else, and a model is provider/name",
			})
			continue
		}
		dir := modelsDir + "/" + provider.Name()
		for _, entry := range entriesOf(layer.FS, dir) {
			file := dir + "/" + entry.Name()
			if bad := notAYAMLFile(entry, file, "a model is one yaml file named after the model"); bad != nil {
				refused = append(refused, *bad)
				continue
			}
			body, err := fs.ReadFile(layer.FS, file)
			if err != nil {
				refused = append(refused, Broken{File: file, Why: err.Error()})
				continue
			}
			slug := provider.Name() + "/" + strings.TrimSuffix(entry.Name(), ".yaml")
			at := sys.Join(layer.Origin, dir, entry.Name())
			if bad := into.take(slug, at, string(body), allowed); bad != nil {
				refused = append(refused, *bad)
			}
		}
	}
	return refused
}

func entriesOf(fsys fs.FS, dir string) []fs.DirEntry {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	return entries
}

func notAYAMLFile(entry fs.DirEntry, file, why string) *Broken {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
		return &Broken{File: file, Why: why}
	}
	return nil
}

func buildSubscription(id string, from *sheet) (SubscriptionSpec, *Broken) {
	spec := SubscriptionSpec{
		ID:        Subscription(id),
		Provider:  Provider(from.values["provider"]),
		Wire:      from.values["wire"],
		Windows:   commas(from.values["windows"]),
		NotModels: commas(from.values["not_models"]),
		File:      from.file,
	}
	if !spec.ID.valid() {
		return spec, &Broken{File: from.file, Why: fmt.Sprintf("%q is not a name a subscription file can carry, a subscription is one word", id)}
	}
	if !spec.Provider.valid() {
		return spec, &Broken{File: from.file, Field: "provider", Why: fmt.Sprintf("the vendor is %s or %s, found %q", Anthropic, OpenAI, spec.Provider)}
	}
	if spec.Wire == "" {
		return spec, &Broken{File: from.file, Field: "wire", Why: "the subscription names no wire, so tofu cannot tell which credential and which protocol reach it"}
	}
	if len(spec.Windows) == 0 {
		return spec, &Broken{File: from.file, Field: "windows", Why: "a subscription spends a quota window rather than money, so it names its windows"}
	}
	return spec, nil
}

func buildModel(slug string, from *sheet, known map[Subscription]SubscriptionSpec) (Model, *Broken) {
	provider, name, _ := strings.Cut(slug, "/")
	model := Model{
		Provider:     Provider(provider),
		ID:           name,
		Subscription: Subscription(from.values["subscription"]),
		Use:          Use(from.values["use"]),
		Reason:       from.values["reason"],
		File:         from.file,
	}
	if !model.Provider.valid() {
		return model, &Broken{File: from.file, Why: fmt.Sprintf("the vendor is %s or %s, found %q", Anthropic, OpenAI, model.Provider)}
	}
	if model.Subscription != "" {
		spec, carried := known[model.Subscription]
		if !carried {
			return model, &Broken{File: from.file, Field: "subscription", Why: fmt.Sprintf("no subscription in the library is called %q", model.Subscription)}
		}
		if model.Provider != spec.Provider {
			return model, &Broken{File: from.file, Why: fmt.Sprintf("the %s subscription is served by %s, so this model is filed under the wrong vendor", spec.ID, spec.Provider)}
		}
		model.Windows = append(append([]string{}, spec.Windows...), commas(from.values["window"])...)
	} else {
		model.Windows = commas(from.values["window"])
	}
	if model.Use == "" {
		model.Use = UseExcluded
		model.Reason = "the entry declares no use, so tofu will not send it until somebody says it may"
		return model, nil
	}
	if !model.Use.valid() {
		return model, &Broken{File: from.file, Field: "use", Why: fmt.Sprintf("use is %s, %s or %s, found %q", UseDefault, UseAllowed, UseExcluded, model.Use)}
	}
	if model.Use == UseExcluded && model.Reason == "" {
		return model, &Broken{File: from.file, Field: "reason", Why: "an excluded entry carries the reason it was excluded"}
	}
	return model, nil
}

func (c Library) defaults() []Broken {
	var refused []Broken
	chosen := map[Subscription]string{}
	served := map[Subscription]bool{}
	for _, model := range c.Models {
		if model.Subscription == "" {
			continue
		}
		served[model.Subscription] = true
		if model.Use != UseDefault {
			continue
		}
		if first, taken := chosen[model.Subscription]; taken {
			refused = append(refused, Broken{File: model.File, Field: "use", Why: "the " + string(model.Subscription) + " subscription already has a default in " + first})
			continue
		}
		chosen[model.Subscription] = model.File
	}
	for _, spec := range c.Subscriptions {
		if served[spec.ID] && chosen[spec.ID] == "" {
			refused = append(refused, Broken{File: spec.File, Why: "the " + string(spec.ID) + " subscription has models and none of them is the default"})
		}
	}
	return refused
}

func commas(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
}
