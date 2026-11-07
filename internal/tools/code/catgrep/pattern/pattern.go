package pattern

import (
	"fmt"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/tree"
)

type shape uint8

const (
	metaVarShape shape = iota + 1
	terminalShape
	internalShape
)

type node struct {
	shape    shape
	metaVar  metavar.Var
	text     string
	named    bool
	kind     gotreesitter.Symbol
	anyKind  bool
	children []node
}

type Pattern struct {
	root       node
	Strictness Strictness
}

type Match struct {
	Node tree.Node
	Env  *metavar.Env
}

func New(src string, l lang.Lang) (Pattern, error) {
	parsed, err := tree.Parse(l.PreprocessPattern(src), l)
	if err != nil {
		return Pattern{}, fmt.Errorf("pattern %q does not parse as %s: %w", src, l, err)
	}
	goal := parsed.Node()
	if len(goal.Children()) == 0 {
		return Pattern{}, fmt.Errorf("pattern %q has no syntax node in %s", src, l)
	}
	if !isSingle(goal) {
		return Pattern{}, fmt.Errorf("pattern %q is more than one syntax node in %s", src, l)
	}
	for isSingle(goal) {
		goal = goal.Children()[0]
	}
	root := convert(goal, sigilOf(l))
	if _, ok := ellipsisOf(&root); ok {
		return Pattern{}, fmt.Errorf("pattern %q in %s is a lone multi meta variable: use $VAR or wrap $$$VAR in a larger pattern", src, l)
	}
	return Pattern{root: root, Strictness: Smart}, nil
}

func sigilOf(l lang.Lang) rune { return []rune(l.PreprocessPattern("$A"))[0] }

func isSingle(n tree.Node) bool {
	children := n.Children()
	switch len(children) {
	case 1:
		return true
	case 2:
		return children[1].IsMissing() || children[1].Kind() == ""
	}
	return false
}

func convert(n tree.Node, sigil rune) node {
	if v, ok := metavar.Extract(n.Text(), sigil); ok {
		return node{shape: metaVarShape, metaVar: v}
	}
	if n.IsLeaf() {
		return node{shape: terminalShape, text: n.Text(), named: n.IsNamed(), kind: n.KindID(), anyKind: n.IsError()}
	}
	var children []node
	for _, c := range n.Children() {
		if !c.IsMissing() {
			children = append(children, convert(c, sigil))
		}
	}
	return node{shape: internalShape, kind: n.KindID(), anyKind: n.IsError(), children: children}
}

func (g *node) matchesKind(candidate tree.Node) bool {
	return g.anyKind || g.kind == candidate.KindID()
}

func ellipsisOf(goal *node) (metavar.Var, bool) {
	if goal.shape == metaVarShape && goal.metaVar.IsEllipsis() {
		return goal.metaVar, true
	}
	return metavar.Var{}, false
}

func (g *node) trivial() bool { return g.shape == terminalShape && !g.named }

func (p Pattern) Match(candidate tree.Node) (*metavar.Env, bool) {
	env := envBinder{metavar.NewEnv()}
	if p.matchRoot(candidate, env) != matchedBoth {
		return nil, false
	}
	return env.env, true
}

func (p Pattern) Find(root tree.Node) (Match, bool) {
	for candidate := range root.PreOrder() {
		if env, ok := p.Match(candidate); ok {
			return Match{Node: candidate, Env: env}, true
		}
	}
	return Match{}, false
}

func (p Pattern) MatchEnd(candidate tree.Node) (uint32, bool) {
	end := &endBinder{}
	if p.matchRoot(candidate, end) != matchedBoth {
		return 0, false
	}
	return end.end, true
}

func (p Pattern) matchRoot(candidate tree.Node, b binder) step {
	if p.root.shape == metaVarShape && p.Strictness == Smart {
		if b.metaVar(p.root.metaVar, candidate) {
			return matchedBoth
		}
		return noMatch
	}
	return matchNode(&p.root, candidate, b, p.Strictness)
}
