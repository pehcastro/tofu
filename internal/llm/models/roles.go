package models

import (
	"errors"
	"path/filepath"

	"tofu/internal/sys"
)

const (
	rolesDir       = "roles"
	legacyTurnRole = "turn"
)

type RoleID string

const (
	RoleOrchestrator RoleID = "orchestrator"
	RoleChild        RoleID = "child"
)

func RoleIDs() []RoleID { return []RoleID{RoleOrchestrator, RoleChild} }

func (r RoleID) valid() bool {
	switch r {
	case RoleOrchestrator, RoleChild:
		return true
	}
	return false
}

func (r RoleID) Label() string {
	switch r {
	case RoleOrchestrator:
		return string(r)
	case RoleChild:
		return "(unnamed sub-agent)"
	}
	panic("models: unknown role " + string(r))
}

func (r RoleID) What() string {
	switch r {
	case RoleOrchestrator:
		return "the model that plans and hands work to sub-agents"
	case RoleChild:
		return "a spawn that names no sub-agent"
	}
	panic("models: unknown role " + string(r))
}

func (r RoleID) Unbound() string {
	switch r {
	case RoleOrchestrator:
		return "nothing is bound, so it runs on the subscription default"
	case RoleChild:
		return "nothing is bound, so it runs on the orchestrator's model"
	}
	panic("models: unknown role " + string(r))
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
		binding.Wire = c.WireFor(binding.Model.Subscription)
		bound[id] = binding
	}
	return bound, nil
}

func buildRole(name string, from *sheet, library Library) (Role, *Broken) {
	id := RoleID(name)
	if name == legacyTurnRole {
		id = RoleOrchestrator
	}
	if !id.valid() {
		return Role{}, &Broken{File: from.file,
			Why: "a role file is " + string(RoleOrchestrator) + ".yaml or " + string(RoleChild) + ".yaml, and nothing else reads one"}
	}
	slug := from.values["model"]
	if slug == "" {
		return Role{}, &Broken{File: from.file, Field: "model", Why: "a role binds one model, written provider/name"}
	}
	model, err := library.Select(slug)
	if err != nil {
		return Role{}, &Broken{File: from.file, Field: "model", Why: err.Error()}
	}
	return Role{ID: id, Model: model, File: from.file}, nil
}

func BindRole(layerDir string, role RoleID, slug string) error {
	if !role.valid() {
		return errors.New("a role file is " + string(RoleOrchestrator) + ".yaml or " + string(RoleChild) + ".yaml, not " + string(role) + ".yaml")
	}
	return sys.WriteFile(filepath.Join(layerDir, rolesDir, string(role)+".yaml"), []byte("model: "+slug+"\n"), 0o644)
}
