package models

const rolesDir = "roles"

type RoleID string

const (
	RoleTurn  RoleID = "turn"
	RoleChild RoleID = "child"
)

func RoleIDs() []RoleID { return []RoleID{RoleTurn, RoleChild} }

func (r RoleID) valid() bool {
	switch r {
	case RoleTurn, RoleChild:
		return true
	}
	return false
}

func (r RoleID) What() string {
	switch r {
	case RoleTurn:
		return "the turn you asked for"
	case RoleChild:
		return "every child a turn spawns"
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
		return string(b.Role) + " runs " + b.Model.Slug() + ", bound by " + b.File
	case BoundByDefault:
		return string(b.Role) + " has nothing bound, so it runs " + b.Model.Slug() +
			", the " + string(b.Model.Subscription) + " default"
	}
	panic("models: unknown binding source")
}

type Bindings map[RoleID]Binding

func (c Catalog) Bind(fallback Subscription) (Bindings, error) {
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
		binding.Wire = c.wireFor(binding.Model.Subscription)
		bound[id] = binding
	}
	return bound, nil
}

func buildRole(name string, from *sheet, catalog Catalog) (Role, *Broken) {
	id := RoleID(name)
	if !id.valid() {
		return Role{}, &Broken{File: from.file,
			Why: "a role is " + string(RoleTurn) + " or " + string(RoleChild) + ", and nothing else reads one"}
	}
	slug := from.values["model"]
	if slug == "" {
		return Role{}, &Broken{File: from.file, Field: "model", Why: "a role binds one model, written provider/name"}
	}
	model, err := catalog.Select(slug)
	if err != nil {
		return Role{}, &Broken{File: from.file, Field: "model", Why: err.Error()}
	}
	return Role{ID: id, Model: model, File: from.file}, nil
}
