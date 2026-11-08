package pattern

import (
	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/tree"
)

type binder interface {
	terminal(candidate tree.Node) bool
	metaVar(v metavar.Var, candidate tree.Node) bool
	ellipsis(v metavar.Var, run span) bool
	probe() binder
}

type span struct {
	parent         tree.Node
	from, to       int
	skippedTrivial int
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

func (b envBinder) ellipsis(v metavar.Var, run span) bool {
	if v.Kind != metavar.MultiCapture {
		return true
	}
	nodes := make([]tree.Node, 0, max(run.to-run.skippedTrivial-run.from, 0))
	for i := run.from; i < run.to-run.skippedTrivial; i++ {
		nodes = append(nodes, run.parent.Child(i))
	}
	return b.env.InsertMulti(v.Name, nodes)
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

func (b *endBinder) ellipsis(_ metavar.Var, run span) bool {
	if run.to == run.from {
		return false
	}
	b.end = run.parent.Child(run.to - 1).End()
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
		if matchChildren(goal.children, candidate, b, s) {
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
	parent     tree.Node
	count      int
	goal, cand int
}

func (c *cursor) goalsLeft() bool { return c.goal < len(c.goals) }

func (c *cursor) candsLeft() bool { return c.cand < c.count }

func (c *cursor) peekGoal() *node { return &c.goals[c.goal] }

func (c *cursor) peekCand() tree.Node { return c.parent.Child(c.cand) }

func (c *cursor) run(from, skippedTrivial int) span {
	return span{parent: c.parent, from: from, to: c.cand, skippedTrivial: skippedTrivial}
}

func matchChildren(goals []node, parent tree.Node, b binder, s Strictness) bool {
	c := &cursor{goals: goals, parent: parent, count: parent.ChildCount()}
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
			c.cand = min(c.cand+1, c.count)
		}
		if !c.goalsLeft() {
			for ; c.candsLeft(); c.cand++ {
				if !s.skipsTrailing(c.peekCand()) {
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
	from := c.cand
	c.goal++
	skipped := 0
	for !c.goalsLeft() || c.peekGoal().trivial() {
		if !c.goalsLeft() {
			c.cand = c.count
			if !b.ellipsis(v, c.run(from, skipped)) {
				return failFlow
			}
			return returnFlow
		}
		c.goal++
		skipped++
	}
	if _, next := ellipsisOf(c.peekGoal()); next {
		c.cand++
		if !c.candsLeft() || !b.ellipsis(v, c.run(from, skipped)) {
			return failFlow
		}
		return continueFlow
	}
	for {
		if matchNode(c.peekGoal(), c.peekCand(), b.probe(), s) == matchedBoth {
			if !b.ellipsis(v, c.run(from, skipped)) {
				return failFlow
			}
			return fallthroughFlow
		}
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
