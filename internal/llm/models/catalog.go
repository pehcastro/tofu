package models

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

type Provider string

const (
	Anthropic Provider = "anthropic"
	Codex     Provider = "codex"
)

func (p Provider) Valid() bool {
	switch p {
	case Anthropic, Codex:
		return true
	}
	return false
}

func Providers() []Provider { return []Provider{Anthropic, Codex} }

type Use string

const (
	UseDefault  Use = "default"
	UseAllowed  Use = "allowed"
	UseExcluded Use = "excluded"
)

func (u Use) valid() bool {
	switch u {
	case UseDefault, UseAllowed, UseExcluded:
		return true
	}
	return false
}

type Model struct {
	ID       string
	Provider Provider
	Windows  []string
	Use      Use
	Reason   string
	File     string
}

func (m Model) WindowText() string { return strings.Join(m.Windows, " and ") }

type Catalog struct {
	Models []Model
}

func Load(shipped fs.FS) (Catalog, error) {
	names, err := fs.Glob(shipped, "*.yaml")
	if err != nil {
		return Catalog{}, err
	}
	sort.Strings(names)
	catalog := Catalog{Models: make([]Model, 0, len(names))}
	for _, name := range names {
		body, err := fs.ReadFile(shipped, name)
		if err != nil {
			return Catalog{}, err
		}
		model, err := parse(name, string(body))
		if err != nil {
			return Catalog{}, err
		}
		catalog.Models = append(catalog.Models, model)
	}
	return catalog, catalog.check()
}

func parse(file, body string) (Model, error) {
	model := Model{File: file}
	for i, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		at := strings.IndexByte(line, ':')
		if at < 0 {
			return Model{}, fmt.Errorf("%s:%d: expected key: value, found %q", file, i+1, line)
		}
		key := strings.TrimSpace(line[:at])
		value := strings.Trim(strings.TrimSpace(line[at+1:]), `"`)
		switch key {
		case "id":
			model.ID = value
		case "provider":
			model.Provider = Provider(value)
		case "window":
			model.Windows = strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
		case "use":
			model.Use = Use(value)
		case "reason":
			model.Reason = value
		default:
			return Model{}, fmt.Errorf("%s:%d: unknown field %q", file, i+1, key)
		}
	}
	if err := model.complete(); err != nil {
		return Model{}, err
	}
	return model, nil
}

func (m *Model) complete() error {
	if m.ID == "" {
		return fmt.Errorf("%s: the entry declares no id", m.File)
	}
	if !m.Provider.Valid() {
		return fmt.Errorf("%s: provider is %q or %q, found %q", m.File, Anthropic, Codex, m.Provider)
	}
	if len(m.Windows) == 0 {
		return fmt.Errorf("%s: the entry names no window, and a subscription model spends a window rather than money", m.File)
	}
	if m.Use == "" {
		m.Use = UseExcluded
		m.Reason = "the entry declares no use, so boji will not send it until somebody says it may"
		return nil
	}
	if !m.Use.valid() {
		return fmt.Errorf("%s: use is %q, %q or %q, found %q", m.File, UseDefault, UseAllowed, UseExcluded, m.Use)
	}
	if m.Use == UseExcluded && m.Reason == "" {
		return fmt.Errorf("%s: an excluded entry carries the reason it was excluded", m.File)
	}
	return nil
}

func (c Catalog) check() error {
	seen := make(map[string]string, len(c.Models))
	defaults := make(map[Provider]string)
	served := make(map[Provider]bool)
	for _, model := range c.Models {
		key := string(model.Provider) + "/" + model.ID
		if first, repeated := seen[key]; repeated {
			return fmt.Errorf("%s: %s is already declared in %s", model.File, key, first)
		}
		seen[key] = model.File
		served[model.Provider] = true
		if model.Use != UseDefault {
			continue
		}
		if first, taken := defaults[model.Provider]; taken {
			return fmt.Errorf("%s: %s already has a default in %s", model.File, model.Provider, first)
		}
		defaults[model.Provider] = model.File
	}
	for provider := range served {
		if defaults[provider] == "" {
			return fmt.Errorf("%s has models and no default", provider)
		}
	}
	return nil
}

type RefusalKind int

const (
	RefusedUnknown RefusalKind = iota
	RefusedExcluded
)

type Refusal struct {
	Kind     RefusalKind
	ID       string
	Provider Provider
	Reason   string
	Known    []string
}

func (r *Refusal) Error() string {
	switch r.Kind {
	case RefusedExcluded:
		return fmt.Sprintf("the model catalog excludes %s model %s: %s", r.Provider, r.ID, r.Reason)
	case RefusedUnknown:
		return fmt.Sprintf("the model catalog has no %s model %s, it has %s",
			r.Provider, r.ID, strings.Join(r.Known, ", "))
	}
	panic("models: unknown refusal kind")
}

func (c Catalog) Select(provider Provider, id string) (Model, error) {
	known := make([]string, 0, len(c.Models))
	for _, model := range c.Models {
		if model.Provider != provider {
			continue
		}
		if model.ID != id {
			if model.Use != UseExcluded {
				known = append(known, model.ID)
			}
			continue
		}
		if model.Use == UseExcluded {
			return Model{}, &Refusal{Kind: RefusedExcluded, ID: id, Provider: provider, Reason: model.Reason}
		}
		return model, nil
	}
	return Model{}, &Refusal{Kind: RefusedUnknown, ID: id, Provider: provider, Known: known}
}

func (c Catalog) Default(provider Provider) (Model, error) {
	for _, model := range c.Models {
		if model.Provider == provider && model.Use == UseDefault {
			return model, nil
		}
	}
	return Model{}, fmt.Errorf("the model catalog has no default for %s", provider)
}

func (c Catalog) Lines() []string {
	lines := make([]string, 0, len(c.Models)+len(Providers()))
	for _, provider := range Providers() {
		lines = append(lines, string(provider))
		for _, model := range c.Models {
			if model.Provider != provider {
				continue
			}
			line := "  " + model.ID + " " + string(model.Use) + ", spends " + model.WindowText()
			if model.Reason != "" {
				line += ", reason: " + model.Reason
			}
			lines = append(lines, line)
		}
	}
	return lines
}
