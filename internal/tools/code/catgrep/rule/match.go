package rule

import (
	"regexp"
	"slices"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/tree"
)

type matcher interface {
	match(n tree.Node, env *metavar.Env) (*metavar.Env, bool)
	kinds() ([]gotreesitter.Symbol, bool)
}

type Rule struct {
	parts []matcher
	only  []gotreesitter.Symbol
	typed bool
}

func newRule(parts []matcher) Rule {
	r := Rule{parts: parts}
	for _, part := range parts {
		kinds, typed := part.kinds()
		switch {
		case !typed:
		case r.typed:
			r.only = slices.DeleteFunc(r.only, func(k gotreesitter.Symbol) bool { return !slices.Contains(kinds, k) })
		default:
			r.only, r.typed = slices.Clone(kinds), true
		}
	}
	return r
}

func (r Rule) Match(n tree.Node) (*metavar.Env, bool) {
	env, ok := r.match(n, nil)
	if ok && env == nil {
		env = metavar.NewEnv()
	}
	return env, ok
}

func (r Rule) kinds() ([]gotreesitter.Symbol, bool) { return r.only, r.typed }

func (r Rule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	if r.typed && !slices.Contains(r.only, n.KindID()) {
		return nil, false
	}
	for _, part := range r.parts {
		var ok bool
		if env, ok = part.match(n, env); !ok {
			return nil, false
		}
	}
	return env, true
}

type patternRule struct{ p pattern.Pattern }

func (r patternRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	env = env.Clone()
	return env, r.p.MatchIn(n, env)
}

func (r patternRule) kinds() ([]gotreesitter.Symbol, bool) {
	kind, typed := r.p.Kind()
	return []gotreesitter.Symbol{kind}, typed
}

type kindRule gotreesitter.Symbol

func (k kindRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	return env, n.IsNamed() && n.KindID() == gotreesitter.Symbol(k)
}

func (k kindRule) kinds() ([]gotreesitter.Symbol, bool) {
	return []gotreesitter.Symbol{gotreesitter.Symbol(k)}, true
}

type untyped struct{}

func (untyped) kinds() ([]gotreesitter.Symbol, bool) { return nil, false }

type regexRule struct {
	untyped
	re *regexp.Regexp
}

func (r regexRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	return env, r.re.Match(n.Bytes())
}

type anyRule []matcher

func (alternatives anyRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	for _, alt := range alternatives {
		if found, ok := alt.match(n, env); ok {
			return found, true
		}
	}
	return nil, false
}

func (alternatives anyRule) kinds() ([]gotreesitter.Symbol, bool) {
	var union []gotreesitter.Symbol
	for _, alt := range alternatives {
		kinds, typed := alt.kinds()
		if !typed {
			return nil, false
		}
		union = append(union, kinds...)
	}
	return union, true
}

type notRule struct {
	untyped
	inner Rule
}

func (r notRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	_, matched := r.inner.match(n, env)
	return env, !matched
}

type stopKind uint8

const (
	neighbor stopKind = iota + 1
	toEnd
	untilRule
)

type relation struct {
	untyped
	rule  Rule
	stop  stopKind
	until Rule
	field string
}

func (r relation) stopsAt(n tree.Node) bool {
	switch r.stop {
	case neighbor:
		return true
	case toEnd:
		return false
	case untilRule:
		_, ok := r.until.match(n, nil)
		return ok
	}
	panic("rule: unknown stopBy")
}

type insideRule struct{ relation }

func (r insideRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	child := n
	for ancestor, ok := n.Parent(); ok; ancestor, ok = ancestor.Parent() {
		if field, has := ancestor.Field(r.field); r.field == "" || has && field.Same(child) {
			if found, ok := r.rule.match(ancestor, env); ok {
				return found, true
			}
		}
		if r.stopsAt(ancestor) {
			break
		}
		child = ancestor
	}
	return nil, false
}

type hasRule struct{ relation }

func (r hasRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	if r.field == "" {
		return r.search(n, env)
	}
	child, ok := n.Field(r.field)
	if !ok {
		return nil, false
	}
	return r.visit(child, env)
}

func (r hasRule) search(parent tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	for i := range parent.ChildCount() {
		if found, ok := r.visit(parent.Child(i), env); ok {
			return found, true
		}
	}
	return nil, false
}

func (r hasRule) visit(child tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	if found, ok := r.rule.match(child, env); ok {
		return found, true
	}
	if r.stopsAt(child) {
		return nil, false
	}
	return r.search(child, env)
}
