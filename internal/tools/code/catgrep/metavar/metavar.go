package metavar

import (
	"bytes"
	"maps"
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
	single      map[string]tree.Node
	multi       map[string][]tree.Node
	transformed map[string]string
}

func NewEnv() *Env { return &Env{} }

func (e *Env) Clone() *Env {
	if e == nil {
		return &Env{}
	}
	return &Env{single: maps.Clone(e.single), multi: maps.Clone(e.multi), transformed: maps.Clone(e.transformed)}
}

func (e *Env) Insert(name string, node tree.Node) bool {
	if bound, ok := e.single[name]; ok && !sameTree(bound, node) {
		return false
	}
	if e.single == nil {
		e.single = map[string]tree.Node{}
	}
	e.single[name] = node
	return true
}

func (e *Env) InsertMulti(name string, nodes []tree.Node) bool {
	if bound, ok := e.multi[name]; ok && !sameNamed(bound, nodes) {
		return false
	}
	if e.multi == nil {
		e.multi = map[string][]tree.Node{}
	}
	e.multi[name] = nodes
	return true
}

func (e *Env) InsertTransformed(name, text string) {
	if e.transformed == nil {
		e.transformed = map[string]string{}
	}
	e.transformed[name] = text
}

func (e *Env) Single(name string) (tree.Node, bool) {
	node, ok := e.single[name]
	return node, ok
}

func (e *Env) Multi(name string) ([]tree.Node, bool) {
	nodes, ok := e.multi[name]
	return nodes, ok
}

func (e *Env) Transformed(name string) (string, bool) {
	text, ok := e.transformed[name]
	return text, ok
}

func sameNamed(bound, nodes []tree.Node) bool {
	unnamed := func(n tree.Node) bool { return !n.IsNamed() }
	return slices.EqualFunc(slices.DeleteFunc(slices.Clone(bound), unnamed), slices.DeleteFunc(slices.Clone(nodes), unnamed), sameTree)
}

func (e *Env) Texts() map[string]string {
	texts := make(map[string]string, len(e.transformed)+len(e.single)+len(e.multi))
	maps.Copy(texts, e.transformed)
	for name, node := range e.single {
		texts[name] = node.Text()
	}
	for name, nodes := range e.multi {
		parts := make([]string, len(nodes))
		for i, n := range nodes {
			parts[i] = n.Text()
		}
		texts[name] = "[" + strings.Join(parts, ", ") + "]"
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
