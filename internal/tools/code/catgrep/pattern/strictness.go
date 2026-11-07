package pattern

import (
	"fmt"

	"tofu/internal/tools/code/catgrep/tree"
)

type Strictness uint8

const (
	Smart Strictness = iota + 1
	Cst
	Ast
	Relaxed
	Signature
	Template
)

func ParseStrictness(name string) (Strictness, error) {
	switch name {
	case "smart":
		return Smart, nil
	case "cst":
		return Cst, nil
	case "ast":
		return Ast, nil
	case "relaxed":
		return Relaxed, nil
	case "signature":
		return Signature, nil
	case "template":
		return Template, nil
	}
	return 0, fmt.Errorf("strictness %q is not one of smart, cst, ast, relaxed, signature, template", name)
}

type step uint8

const (
	matchedBoth step = iota + 1
	skipBoth
	skipGoal
	skipCandidate
	noMatch
)

func (s Strictness) skipsComments() bool {
	switch s {
	case Cst, Ast:
		return false
	case Smart, Relaxed, Signature, Template:
		return true
	}
	panic("pattern: unknown strictness")
}

func (s Strictness) matchTerminal(goal *node, candidate tree.Node) step {
	kindMatched := goal.matchesKind(candidate)
	if kindMatched && (!goal.named || goal.text == candidate.Text()) {
		return matchedBoth
	}
	if s.skipsComments() && candidate.IsExtra() {
		return skipCandidate
	}
	var skipsGoal, skipsCandidate bool
	switch s {
	case Cst:
	case Smart:
		skipsCandidate = !candidate.IsNamed()
	case Ast, Relaxed:
		skipsGoal, skipsCandidate = !goal.named, !candidate.IsNamed()
	case Signature:
		if kindMatched {
			return matchedBoth
		}
		skipsGoal, skipsCandidate = !goal.named, !candidate.IsNamed()
	case Template:
		if goal.text == candidate.Text() {
			return matchedBoth
		}
		skipsCandidate = !candidate.IsNamed()
	default:
		panic("pattern: unknown strictness")
	}
	switch {
	case skipsGoal && skipsCandidate:
		return skipBoth
	case skipsGoal:
		return skipGoal
	case skipsCandidate:
		return skipCandidate
	}
	return noMatch
}

func (s Strictness) skipsTrailing(candidate tree.Node) bool {
	switch s {
	case Cst:
		return false
	case Smart:
		return true
	case Ast:
		return !candidate.IsNamed()
	case Relaxed, Signature:
		return !candidate.IsNamed() || candidate.IsExtra()
	case Template:
		return candidate.IsExtra()
	}
	panic("pattern: unknown strictness")
}

func (s Strictness) skipsGoal(goal *node) bool {
	switch s {
	case Cst:
		return false
	case Smart, Template:
		return goal.shape == metaVarShape && goal.metaVar.IsEllipsis()
	case Ast, Relaxed, Signature:
		switch goal.shape {
		case metaVarShape:
			return goal.metaVar.IsEllipsis() || !goal.metaVar.NamedOnly
		case terminalShape:
			return !goal.named
		case internalShape:
			return false
		}
	}
	panic("pattern: unknown strictness")
}
