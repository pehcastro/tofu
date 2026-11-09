package metavar

import (
	"bytes"
	"slices"
	"strings"

	"tofu/internal/tools/code/catgrep/tree"
)

type Kind uint8

const (
	Capture Kind = iota + 1
	Dropped
	Multiple
	MultiCapture
)

type Var struct {
	Kind      Kind
	Name      string
	NamedOnly bool
}

const ellipsisLen = 3

func Extract(src string, sigil rune) (Var, bool) {
	ellipsis := strings.Repeat(string(sigil), ellipsisLen)
	if src == ellipsis {
		return Var{Kind: Multiple}, true
	}
	if rest, ok := strings.CutPrefix(src, ellipsis); ok {
		if !validName(rest) {
			return Var{}, false
		}
		if rest[0] == '_' {
			return Var{Kind: Multiple}, true
		}
		return Var{Kind: MultiCapture, Name: rest}, true
	}
	rest, ok := strings.CutPrefix(src, string(sigil))
	if !ok {
		return Var{}, false
	}
	rest, double := strings.CutPrefix(rest, string(sigil))
	if !validName(rest) {
		return Var{}, false
	}
	if rest[0] == '_' {
		return Var{Kind: Dropped, NamedOnly: !double}, true
	}
	return Var{Kind: Capture, Name: rest, NamedOnly: !double}, true
}

func validName(name string) bool {
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for _, c := range name {
		allowed := c >= 'A' && c <= 'Z' || c == '_' || c >= '0' && c <= '9'
		if !allowed {
			return false
		}
	}
	return true
}

func (v Var) IsEllipsis() bool { return v.Kind == Multiple || v.Kind == MultiCapture }

type Env struct {
	single      []binding[tree.Node]
	multi       []binding[[]tree.Node]
	transformed []binding[string]
}

type binding[T any] struct {
	name  string
	value T
}

func lookup[T any](bound []binding[T], name string) (T, bool) {
	for _, b := range bound {
		if b.name == name {
			return b.value, true
		}
	}
	var none T
	return none, false
}

func bind[T any](bound []binding[T], name string, value T) []binding[T] {
	for i := range bound {
		if bound[i].name == name {
			bound[i].value = value
			return bound
		}
	}
	return append(bound, binding[T]{name: name, value: value})
}

func NewEnv() *Env { return &Env{} }

func (e *Env) Clone() *Env {
	if e == nil {
		return &Env{}
	}
	return &Env{single: slices.Clone(e.single), multi: slices.Clone(e.multi), transformed: slices.Clone(e.transformed)}
}

func (e *Env) Insert(name string, node tree.Node) bool {
	if bound, ok := lookup(e.single, name); ok && !sameTree(bound, node) {
		return false
	}
	e.single = bind(e.single, name, node)
	return true
}

func (e *Env) InsertMulti(name string, nodes []tree.Node) bool {
	if bound, ok := lookup(e.multi, name); ok && !sameNamed(bound, nodes) {
		return false
	}
	e.multi = bind(e.multi, name, nodes)
	return true
}

func (e *Env) InsertTransformed(name, text string) {
	e.transformed = bind(e.transformed, name, text)
}

func (e *Env) Single(name string) (tree.Node, bool) { return lookup(e.single, name) }

func (e *Env) Multi(name string) ([]tree.Node, bool) { return lookup(e.multi, name) }

func (e *Env) Transformed(name string) (string, bool) { return lookup(e.transformed, name) }

func sameNamed(bound, nodes []tree.Node) bool {
	unnamed := func(n tree.Node) bool { return !n.IsNamed() }
	return slices.EqualFunc(slices.DeleteFunc(slices.Clone(bound), unnamed), slices.DeleteFunc(slices.Clone(nodes), unnamed), sameTree)
}

func (e *Env) Texts() map[string]string {
	texts := make(map[string]string, len(e.transformed)+len(e.single)+len(e.multi))
	for _, b := range e.transformed {
		texts[b.name] = b.value
	}
	for _, b := range e.single {
		texts[b.name] = b.value.Text()
	}
	for _, b := range e.multi {
		parts := make([]string, len(b.value))
		for i, n := range b.value {
			parts[i] = n.Text()
		}
		texts[b.name] = "[" + strings.Join(parts, ", ") + "]"
	}
	return texts
}

func sameTree(goal, candidate tree.Node) bool {
	if goal.Same(candidate) {
		return true
	}
	if goal.IsNamedLeaf() || candidate.IsNamedLeaf() {
		return bytes.Equal(goal.Bytes(), candidate.Bytes())
	}
	if goal.KindID() != candidate.KindID() || goal.ChildCount() != candidate.ChildCount() {
		return false
	}
	for i := range goal.ChildCount() {
		if !sameTree(goal.Child(i), candidate.Child(i)) {
			return false
		}
	}
	return true
}
