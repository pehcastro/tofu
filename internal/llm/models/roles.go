package models

import (
	"errors"
	"path/filepath"

	"tofu/internal/sys"
)

const (
	rolesDir             = "roles"
	legacyTurnRole       = "turn"
	legacySubAgentRole   = "child"
	openRouterClassifier = string(OpenRouter) + "/jev-latest"
	typeSafeClassifier   = string(TypeSafe) + "/jev-latest"
	roleFileNames        = "a role file is orchestrator.yaml, classifier.yaml or sub-agent.yaml"
)

type RoleID string

const (
	RoleOrchestrator RoleID = "orchestrator"
	RoleClassifier   RoleID = "classifier"
	RoleSubAgent     RoleID = "sub-agent"
)

func RoleIDs() []RoleID { return []RoleID{RoleOrchestrator, RoleClassifier, RoleSubAgent} }

func (r RoleID) valid() bool {
	switch r {
	case RoleOrchestrator, RoleClassifier, RoleSubAgent:
		return true
	}
	return false
}

func (r RoleID) Takes() Kind {
	switch r {
	case RoleOrchestrator, RoleSubAgent:
		return KindLLM
	case RoleClassifier:
		return KindClassifier
	}
	panic("models: unknown role " + string(r))
}

func (r RoleID) Label() string {
	switch r {
	case RoleOrchestrator, RoleClassifier:
		return string(r)
	case RoleSubAgent:
		return "(unnamed sub-agent)"
	}
	panic("models: unknown role " + string(r))
}

func (r RoleID) What() string {
	switch r {
	case RoleOrchestrator:
		return "the model that plans and hands work to sub-agents"
	case RoleClassifier:
		return "the typed model that judges the model's calls, shell results, browser steps and stop checks"
	case RoleSubAgent:
		return "a spawn that names no sub-agent"
	}
	panic("models: unknown role " + string(r))
}

func (r RoleID) Unbound() string {
	switch r {
	case RoleOrchestrator:
		return "nothing is bound, so it runs on the subscription default"
	case RoleClassifier:
		return "nothing is bound, so it runs " + openRouterClassifier + " when an OpenRouter key is stored, else " + typeSafeClassifier + " when a TypeSafe key is stored"
	case RoleSubAgent:
		return "nothing is bound, so it runs on the orchestrator's model"
	}
	panic("models: unknown role " + string(r))
}

func (c Library) Classifier(stored map[string]string) (Model, error) {
	for _, role := range c.Roles {
		if role.ID == RoleClassifier {
			return role.Model, nil
		}
	}
	if stored[sys.OpenRouterKeyName] == "" && stored[sys.TypeSafeKeyName] != "" {
		return c.Select(typeSafeClassifier)
	}
	return c.Select(openRouterClassifier)
}

type Role struct {
	ID    RoleID
	Model Model
	File  string
}

type BoundBy int

const (
	BoundByFile BoundBy = iota
	BoundByDefault
)

type Binding struct {
	Role  RoleID
	Model Model
	Wire  string
	By    BoundBy
	File  string
}

func (b Binding) Says() string {
	switch b.By {
	case BoundByFile:
		return b.Role.Label() + " runs " + b.Model.Slug() + ", bound by " + b.File
	case BoundByDefault:
		return b.Role.Label() + " has nothing bound, so it runs " + b.Model.Slug() +
			", the " + string(b.Model.Subscription) + " default"
	}
	panic("models: unknown binding source")
}

type Bindings map[RoleID]Binding

func (c Library) Bind(fallback Subscription) (Bindings, error) {
	declared := make(map[RoleID]Role, len(c.Roles))
	for _, role := range c.Roles {
		declared[role.ID] = role
	}
	bound := make(Bindings, len(RoleIDs()))
	for _, id := range RoleIDs() {
		if id.Takes() != KindLLM {
			continue
		}
		binding := Binding{Role: id, By: BoundByDefault}
		if role, named := declared[id]; named {
			binding = Binding{Role: id, Model: role.Model, By: BoundByFile, File: role.File}
		} else {
			model, err := c.Default(fallback)
			if err != nil {
				return nil, err
			}
			binding.Model = model
		}
		binding.Wire = c.WireOf(binding.Model)
		bound[id] = binding
	}
	return bound, nil
}

func buildRole(name string, from *sheet, library Library) (Role, *Broken) {
	id := RoleID(name)
	switch name {
	case legacyTurnRole:
		id = RoleOrchestrator
	case legacySubAgentRole:
		id = RoleSubAgent
	}
	if !id.valid() {
		return Role{}, &Broken{File: from.file, Why: roleFileNames + ", and nothing else reads one"}
	}
	slug := from.values["model"]
	if slug == "" {
		return Role{}, &Broken{File: from.file, Field: "model", Why: "a role binds one model, written provider/name"}
	}
	model, err := library.Select(slug)
	if err != nil {
		return Role{}, &Broken{File: from.file, Field: "model", Why: err.Error()}
	}
	if model.Kind != id.Takes() {
		return Role{}, &Broken{File: from.file, Field: "model", Why: "the " + string(id) + " role runs a " + string(id.Takes()) + " model, and " + slug + " is a " + string(model.Kind)}
	}
	return Role{ID: id, Model: model, File: from.file}, nil
}

func BindRole(layerDir string, role RoleID, slug string) error {
	if !role.valid() {
		return errors.New(roleFileNames + ", not " + string(role) + ".yaml")
	}
	return sys.WriteFile(filepath.Join(layerDir, rolesDir, string(role)+".yaml"), []byte("model: "+slug+"\n"), 0o644)
}
