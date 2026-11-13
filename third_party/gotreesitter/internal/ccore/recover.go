package ccore

import (
	"bytes"
	"math"
)

const (
	costPerRecovery    = 500
	costPerMissingTree = 110
	costPerSkippedTree = 100
	costPerSkippedLine = 30
	costPerSkippedChar = 1
	maxSummaryDepth    = 16
	maxCostDifference  = 18 * costPerSkippedTree
)

type versionStatus struct {
	cost, nodeCount uint32
	dynPrec         int32
	inError         bool
}

type comparison uint8

const (
	equalVersions comparison = iota
	takeLeft
	preferLeft
	preferRight
	takeRight
)

func compareVersions(a, b versionStatus) comparison {
	switch {
	case !a.inError && b.inError:
		if a.cost < b.cost {
			return takeLeft
		}
		return preferLeft
	case a.inError && !b.inError:
		if b.cost < a.cost {
			return takeRight
		}
		return preferRight
	case a.cost < b.cost:
		if (b.cost-a.cost)*(1+a.nodeCount) > maxCostDifference {
			return takeLeft
		}
		return preferLeft
	case b.cost < a.cost:
		if (a.cost-b.cost)*(1+b.nodeCount) > maxCostDifference {
			return takeRight
		}
		return preferRight
	case a.dynPrec > b.dynPrec:
		return preferLeft
	case b.dynPrec > a.dynPrec:
		return preferRight
	}
	return equalVersions
}

func (p *parser) statusOf(v int) versionStatus {
	cost := p.s.errorCost(v)
	isPaused := p.s.heads[v].status == paused
	if isPaused {
		cost += costPerSkippedTree
	}
	return versionStatus{cost: cost, nodeCount: p.s.nodeCountSinceError(v), dynPrec: p.s.node(v).dynPrec, inError: isPaused || p.s.node(v).state == errorState}
}

func (p *parser) betterVersionExists(v int, inError bool, cost uint32) bool {
	if p.finished != 0 && p.r.Trees[p.finished].ErrorCost() <= cost {
		return true
	}
	position := p.s.node(v).position
	mine := versionStatus{cost: cost, inError: inError, dynPrec: p.s.node(v).dynPrec, nodeCount: p.s.nodeCountSinceError(v)}
	for i := range p.s.heads {
		if i == v || p.s.heads[i].status != active || p.s.node(i).position < position {
			continue
		}
		switch compareVersions(mine, p.statusOf(i)) {
		case takeRight:
			return true
		case preferRight:
			if p.s.canMerge(i, v) {
				return true
			}
		}
	}
	return false
}

func (p *parser) condense() uint32 {
	s := &p.s
	least := uint32(math.MaxUint32)
	for i := 0; i < len(s.heads); i++ {
		if s.heads[i].status == halted {
			s.remove(i)
			i--
			continue
		}
		si := p.statusOf(i)
		if !si.inError && si.cost < least {
			least = si.cost
		}
		for j := 0; j < i; j++ {
			switch c := compareVersions(p.statusOf(j), si); c {
			case takeLeft:
				s.remove(i)
				i--
				j = i
			case preferLeft, equalVersions, preferRight:
				if s.merge(j, i) {
					i--
					j = i
				} else if c == preferRight {
					s.swap(i, j)
				}
			case takeRight:
				s.remove(j)
				i--
				j--
			}
		}
	}
	for len(s.heads) > maxVersionCount {
		s.remove(maxVersionCount)
	}
	unpaused := false
	for i := 0; i < len(s.heads); i++ {
		if s.heads[i].status != paused {
			unpaused = true
			continue
		}
		if !unpaused && p.accepts < maxVersionCount {
			least = s.errorCost(i)
			p.handleError(i, s.resume(i))
			unpaused = true
			continue
		}
		s.remove(i)
		i--
	}
	return least
}

func (p *parser) handleError(v int, lookahead uint32) {
	s := &p.s
	previous := len(s.heads)
	p.reduceAll(v, 0)
	count := len(s.heads)
	inserted := false
	for w := v; w < count; {
		if !inserted {
			inserted = p.insertMissing(w, p.r.Trees[lookahead].Symbol)
		}
		s.push(w, 0, errorState)
		if w == v {
			w = previous
		} else {
			w++
		}
	}
	for range count - previous {
		s.merge(v, previous)
	}
	s.summarize(v, maxSummaryDepth)
	p.recover(v, lookahead)
}

func (p *parser) insertMissing(v int, lookahead uint16) bool {
	state := p.s.node(v).state
	for missing := uint16(1); missing < uint16(p.t.TokenCount); missing++ {
		after := p.t.nextState(state, missing)
		if after == 0 || after == state || !p.t.hasReduce(after, lookahead) {
			continue
		}
		copied := p.s.copyVersion(v)
		p.s.push(copied, p.r.leaf(missing, 0, 0, 0, missingToken), after)
		if p.reduceAll(copied, lookahead) {
			return true
		}
	}
	return false
}

func (p *parser) addReduction(a Action) {
	for _, r := range p.reductions {
		if r.Symbol == a.Symbol && r.ChildCount == a.ChildCount {
			return
		}
	}
	p.reductions = append(p.reductions, a)
}

func (p *parser) reduceAll(start int, lookahead uint16) bool {
	s := &p.s
	initial := len(s.heads)
	canShift := false
	v := start
	for i := 0; ; i++ {
		count := len(s.heads)
		if v >= count {
			return canShift
		}
		merged := false
		for j := initial; j < v && !merged; j++ {
			merged = s.merge(j, v)
		}
		if merged {
			continue
		}
		state := s.node(v).state
		shifts := false
		p.reductions = p.reductions[:0]
		first, end := lookahead, lookahead+1
		if lookahead == 0 {
			first, end = 1, uint16(p.t.TokenCount)
		}
		for symbol := first; symbol < end; symbol++ {
			for _, a := range p.t.entry(state, symbol).Actions {
				switch a.Type {
				case ActionShift, ActionRecover:
					shifts = shifts || !a.Extra && !a.Repetition
				case ActionReduce:
					if a.ChildCount > 0 {
						p.addReduction(a)
					}
				}
			}
		}
		reduced := noVersion
		for _, a := range p.reductions {
			reduced = p.reduce(v, a.Symbol, uint32(a.ChildCount), int32(a.DynPrec), a.ProductionID, false)
		}
		switch {
		case shifts:
			canShift = true
		case reduced != noVersion && i < maxVersionCount:
			s.renumber(reduced, v)
			continue
		case lookahead != 0:
			s.remove(v)
		}
		if v == start {
			v = count
		} else {
			v++
		}
	}
}

func (p *parser) recoverFromSummary(v int, symbol uint16, cost, sinceError uint32) bool {
	s := &p.s
	position := s.node(v).position
	previous := len(s.heads)
summary:
	for _, e := range s.summaries[s.heads[v].summary] {
		if e.state == errorState || e.position == position {
			continue
		}
		for j := range previous {
			if s.node(j).state == e.state && s.node(j).position == position {
				continue summary
			}
		}
		skipped := cost + e.depth*costPerSkippedTree + (position-e.position)*costPerSkippedChar + p.rows(e.position, position)*costPerSkippedLine
		if p.betterVersionExists(v, false, skipped) {
			return false
		}
		depth := e.depth
		if sinceError > 0 {
			depth++
		}
		if p.t.lookup(e.state, symbol) != 0 && p.recoverToState(v, depth, e.state) {
			return true
		}
	}
	return false
}

func (p *parser) recover(v int, lookahead uint32) {
	s := &p.s
	previous := len(s.heads)
	position := s.node(v).position
	sinceError := s.nodeCountSinceError(v)
	cost := s.errorCost(v)
	la := p.r.Trees[lookahead]
	recovered := la.Symbol != p.t.errorSymbol() && p.recoverFromSummary(v, la.Symbol, cost, sinceError)
	for i := previous; i < len(s.heads); i++ {
		if s.heads[i].status != active {
			s.remove(i)
			i--
		}
	}
	if la.Symbol == symEnd {
		root := p.r.node(p.t.errorSymbol(), nil, 0)
		p.chargeError(root, position)
		s.push(v, root, initialState)
		p.accept(v, lookahead)
		return
	}
	skipped := cost + costPerSkippedTree + la.Total()*costPerSkippedChar + p.rows(position, position+la.Total())*costPerSkippedLine
	if recovered && (len(s.heads) > maxVersionCount || la.kind == scanChangedToken) || p.betterVersionExists(v, false, skipped) {
		s.heads[v].status = halted
		return
	}
	if actions := p.t.entry(initialState, la.Symbol).Actions; len(actions) > 0 && actions[len(actions)-1].Type == ActionShift && actions[len(actions)-1].Extra {
		lookahead = p.r.copyOf(lookahead)
		p.r.Trees[lookahead].Extra = true
	}
	alone := append(s.buffer(), lookahead)
	repeat := p.r.node(p.t.errorRepeatSymbol(), alone, 0)
	s.recycle(alone)
	p.chargeError(repeat, position)
	if sinceError > 0 {
		popped := s.pop(v, 1, false)
		kept := popped[0]
		for _, sl := range popped[1:] {
			s.recycle(sl.subtrees)
		}
		for len(s.heads) > kept.version+1 {
			s.remove(kept.version + 1)
		}
		s.renumber(kept.version, v)
		kept.subtrees = append(kept.subtrees, repeat)
		repeat = p.r.node(p.t.errorRepeatSymbol(), kept.subtrees, 0)
		s.recycle(kept.subtrees)
		p.chargeError(repeat, s.node(v).position)
	}
	s.push(v, repeat, errorState)
	if p.r.Trees[lookahead].scan() != 0 {
		s.heads[v].external = lookahead
	}
}

func (p *parser) recoverToState(v int, depth uint32, goal uint16) bool {
	s := &p.s
	previous := noVersion
	for _, sl := range s.pop(v, depth, false) {
		sv, trees := sl.version, sl.subtrees
		if sv == previous {
			s.recycle(trees)
			continue
		}
		if s.node(sv).state != goal {
			s.heads[sv].status = halted
			s.recycle(trees)
			continue
		}
		if e := s.popError(sv); e != 0 && p.r.Trees[e].ChildCount > 0 {
			nested := p.r.node(p.t.errorRepeatSymbol(), p.r.Children(e), 0)
			p.chargeError(nested, s.node(sv).position)
			withNested := append(append(s.buffer(), nested), trees...)
			s.recycle(trees)
			trees = withNested
		}
		var kept []uint32
		kept, p.trailing = p.splitTrailingExtras(trees, p.trailing)
		if len(kept) > 0 {
			e := p.r.node(p.t.errorSymbol(), kept, 0)
			p.chargeError(e, s.node(sv).position)
			p.r.Trees[e].Extra = true
			s.push(sv, e, goal)
		}
		for _, t := range p.trailing {
			s.push(sv, t, goal)
		}
		s.recycle(trees)
		previous = sv
	}
	return previous != noVersion
}

func (p *parser) chargeError(id, start uint32) {
	r := &p.r
	at, cost := start, uint32(0)
	for _, c := range r.Children(id) {
		ct := &r.Trees[c]
		switch {
		case ct.Symbol == p.t.errorRepeatSymbol():
			cost -= p.extent(at, ct)
		case ct.Extra || ct.Symbol == p.t.errorSymbol() && ct.ChildCount == 0:
		case p.t.Visible[ct.Symbol]:
			cost += costPerSkippedTree
		case ct.ChildCount > 0:
			cost += costPerSkippedTree * r.visibleChildCount(c)
		}
		at += ct.Total()
	}
	r.Trees[id].errorCost += cost + p.extent(start, &r.Trees[id])
}

func (p *parser) extent(start uint32, t *Subtree) uint32 {
	return costPerRecovery + t.Size*costPerSkippedChar + p.rows(start+t.Padding, start+t.Total())*costPerSkippedLine
}

func (p *parser) rows(from, to uint32) uint32 {
	return uint32(bytes.Count(p.l.src[from:to], []byte{'\n'}))
}
