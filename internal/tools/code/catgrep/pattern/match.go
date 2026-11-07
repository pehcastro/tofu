package pattern

import (
	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/tree"
)

type binder interface {
	terminal(candidate tree.Node) bool
	metaVar(v metavar.Var, candidate tree.Node) bool
	ellipsis(v metavar.Var, nodes []tree.Node, skippedTrivial int) bool
	probe() binder
}

type envBinder struct{ env *metavar.Env }

func (b envBinder) terminal(tree.Node) bool { return true }

func (b envBinder) metaVar(v metavar.Var, candidate tree.Node) bool {
	if v.NamedOnly && !candidate.IsNamed() {
		return false
	}
	switch v.Kind {
	case metavar.Capture:
		return b.env.Insert(v.Name, candidate)
	case metavar.MultiCapture:
		return b.env.InsertMulti(v.Name, []tree.Node{candidate})
	case metavar.Dropped, metavar.Multiple:
		return true
	}
	panic("pattern: unknown meta variable kind")
}

func (b envBinder) ellipsis(v metavar.Var, nodes []tree.Node, skippedTrivial int) bool {
	if v.Kind != metavar.MultiCapture {
		return true
	}
	return b.env.InsertMulti(v.Name, nodes[:max(len(nodes)-skippedTrivial, 0)])
}

func (b envBinder) probe() binder { return envBinder{b.env.Clone()} }

type endBinder struct{ end uint32 }

func (b *endBinder) terminal(candidate tree.Node) bool {
	b.end = candidate.End()
	return true
}

func (b *endBinder) metaVar(_ metavar.Var, candidate tree.Node) bool {
	b.end = candidate.End()
	return true
}

func (b *endBinder) ellipsis(_ metavar.Var, nodes []tree.Node, _ int) bool {
	if len(nodes) == 0 {
		return false
	}
	b.end = nodes[len(nodes)-1].End()
	return true
}

func (b *endBinder) probe() binder { return &endBinder{end: b.end} }

func matchNode(goal *node, candidate tree.Node, b binder, s Strictness) step {
	switch goal.shape {
	case terminalShape:
		got := s.matchTerminal(goal, candidate)
		if got == matchedBoth && !b.terminal(candidate) {
			return noMatch
		}
		return got
	case metaVarShape:
		if s.skipsComments() && candidate.IsExtra() {
			return skipCandidate
		}
		if b.metaVar(goal.metaVar, candidate) {
			return matchedBoth
		}
		return noMatch
	case internalShape:
		if s != Template && !goal.matchesKind(candidate) {
			return noMatch
		}
		if matchChildren(goal.children, candidate.Children(), b, s) {
			return matchedBoth
		}
		return noMatch
	}
	panic("pattern: unknown pattern node shape")
}

type flow uint8

const (
	fallthroughFlow flow = iota + 1
	continueFlow
	returnFlow
	failFlow
)

type cursor struct {
	goals      []node
	candidates []tree.Node
	goal, cand int
}

func (c *cursor) goalsLeft() bool { return c.goal < len(c.goals) }

func (c *cursor) candsLeft() bool { return c.cand < len(c.candidates) }

func (c *cursor) peekGoal() *node { return &c.goals[c.goal] }

func (c *cursor) peekCand() tree.Node { return c.candidates[c.cand] }

func (c *cursor) restCands() []tree.Node {
	rest := c.candidates[c.cand:]
	c.cand = len(c.candidates)
	return rest
}

func matchChildren(goals []node, candidates []tree.Node, b binder, s Strictness) bool {
	c := &cursor{goals: goals, candidates: candidates}
	if !c.candsLeft() {
		return false
	}
	for {
		switch matchEllipsis(c, b, s) {
		case failFlow:
			return false
		case returnFlow:
			return true
		case continueFlow:
			continue
		}
		switch matchSkippingTrivial(c, b, s) {
		case failFlow:
			return false
		case returnFlow:
			return true
		case continueFlow:
			continue
		}
		if c.goalsLeft() {
			c.goal++
			c.cand = min(c.cand+1, len(c.candidates))
		}
		if !c.goalsLeft() {
			for _, rest := range c.restCands() {
				if !s.skipsTrailing(rest) {
					return false
				}
			}
			return true
		}
		if !c.candsLeft() {
			return false
		}
	}
}

func matchEllipsis(c *cursor, b binder, s Strictness) flow {
	if !c.goalsLeft() {
		return returnFlow
	}
	v, ok := ellipsisOf(c.peekGoal())
	if !ok {
		return fallthroughFlow
	}
	var matched []tree.Node
	c.goal++
	skipped := 0
	for !c.goalsLeft() || c.peekGoal().trivial() {
		if !c.goalsLeft() {
			if !b.ellipsis(v, append(matched, c.restCands()...), skipped) {
				return failFlow
			}
			return returnFlow
		}
		c.goal++
		skipped++
	}
	if _, next := ellipsisOf(c.peekGoal()); next {
		matched = append(matched, c.peekCand())
		c.cand++
		if !c.candsLeft() || !b.ellipsis(v, matched, skipped) {
			return failFlow
		}
		return continueFlow
	}
	for {
		if matchNode(c.peekGoal(), c.peekCand(), b.probe(), s) == matchedBoth {
			if !b.ellipsis(v, matched, skipped) {
				return failFlow
			}
			return fallthroughFlow
		}
		matched = append(matched, c.peekCand())
		c.cand++
		if !c.candsLeft() {
			return failFlow
		}
	}
}

func matchSkippingTrivial(c *cursor, b binder, s Strictness) flow {
	for {
		if !c.candsLeft() {
			for c.goalsLeft() && s.skipsGoal(c.peekGoal()) {
				c.goal++
			}
			if c.goalsLeft() {
				return failFlow
			}
			return fallthroughFlow
		}
		switch matchNode(c.peekGoal(), c.peekCand(), b, s) {
		case matchedBoth:
			return fallthroughFlow
		case skipGoal:
			c.goal++
			if !c.goalsLeft() {
				return fallthroughFlow
			}
		case skipBoth:
			c.cand++
			c.goal++
			if !c.goalsLeft() {
				return fallthroughFlow
			}
		case skipCandidate:
			c.cand++
		case noMatch:
			return failFlow
		}
	}
}
