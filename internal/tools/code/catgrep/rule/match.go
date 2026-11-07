package rule

import (
	"regexp"

	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/tree"
)

type matcher interface {
	match(n tree.Node, env *metavar.Env) (*metavar.Env, bool)
}

type Rule struct{ parts []matcher }

func (r Rule) Match(n tree.Node) (*metavar.Env, bool) { return r.match(n, metavar.NewEnv()) }

func (r Rule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
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

type kindRule string

func (k kindRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	return env, n.IsNamed() && n.Kind() == string(k)
}

type regexRule struct{ re *regexp.Regexp }

func (r regexRule) match(n tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	return env, r.re.MatchString(n.Text())
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

type notRule struct{ inner Rule }

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
		_, ok := r.until.Match(n)
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
		return r.search(n.Children(), env)
	}
	child, ok := n.Field(r.field)
	if !ok {
		return nil, false
	}
	return r.search([]tree.Node{child}, env)
}

func (r hasRule) search(children []tree.Node, env *metavar.Env) (*metavar.Env, bool) {
	for _, child := range children {
		if found, ok := r.rule.match(child, env); ok {
			return found, true
		}
		if r.stopsAt(child) {
			continue
		}
		if found, ok := r.search(child.Children(), env); ok {
			return found, true
		}
	}
	return nil, false
}
