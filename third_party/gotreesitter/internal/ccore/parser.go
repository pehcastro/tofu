package ccore

type Status uint8

const (
	Accepted Status = iota
	Declined
	Stopped
)

const (
	maxVersionCount         = 6
	maxVersionCountOverflow = 4
	opsPerStopCheck         = 100
	initialState            = 1
	sourceBytesPerTree      = 2
	serializationBufferSize = 1024
)

type parser struct {
	t             *Tables
	r             Result
	s             stack
	l             lexer
	scanner       Scanner
	stop          func() bool
	cacheToken    uint32
	cacheByte     uint32
	cacheExternal uint32
	finished      uint32
	accepts       int
	ops           int
	outcome       Status
	trailing      []uint32
	trailing2     []uint32
	scanBuf       [serializationBufferSize]byte
}

func Parse(t *Tables, src []byte, scanner Scanner, stop func() bool) (Result, Status) {
	p := &parser{t: t, stop: stop, scanner: scanner, l: lexer{src: src}}
	estimate := len(src)/sourceBytesPerTree + 1
	p.r.Trees = make([]Subtree, 1, estimate)
	p.r.Kids = make([]uint32, 0, estimate)
	p.r.scans = []scanState{{}}
	p.s = stack{r: &p.r, nodes: make([]stackNode, 1, 64)}
	p.s.heads = []head{{node: p.s.newNode(0, 0, initialState)}}
	if !p.run() {
		return Result{}, p.outcome
	}
	p.r.Root = p.finished
	return p.r, Accepted
}

func (p *parser) run() bool {
	last := uint32(0)
	for {
		for v := 0; v < len(p.s.heads); v++ {
			for p.s.heads[v].status == active {
				if !p.advance(v) {
					return false
				}
				position := p.s.node(v).position.Bytes
				if position > last || v > 0 && position == last {
					last = position
					break
				}
			}
		}
		sawUnpaused, needsRecovery := p.condense()
		if p.finished != 0 && (!sawUnpaused || needsRecovery) {
			return true
		}
		if needsRecovery || len(p.s.heads) == 0 {
			return p.decline()
		}
	}
}

func (p *parser) condense() (sawUnpaused, needsRecovery bool) {
	for i := 0; i < len(p.s.heads); i++ {
		if p.s.heads[i].status == halted {
			p.s.remove(i)
			i--
			continue
		}
		if p.s.heads[i].status == active {
			sawUnpaused = true
		}
		for j := 0; j < i; j++ {
			switch p.compare(j, i) {
			case takeLeft:
				p.s.remove(i)
				i--
				j = i
			case preferLeft, none:
				if p.s.merge(j, i) {
					i--
					j = i
				}
			case preferRight:
				if p.s.merge(j, i) {
					i--
					j = i
				} else {
					p.s.swap(i, j)
				}
			case takeRight:
				p.s.remove(j)
				i--
				j--
			}
		}
	}
	for len(p.s.heads) > maxVersionCount {
		p.s.remove(maxVersionCount)
	}
	hasUnpaused := false
	for i := 0; i < len(p.s.heads); i++ {
		if p.s.heads[i].status != paused {
			hasUnpaused = true
			continue
		}
		if !hasUnpaused && p.accepts < maxVersionCount {
			return sawUnpaused, true
		}
		p.s.remove(i)
		i--
	}
	return sawUnpaused, false
}

type comparison uint8

const (
	none comparison = iota
	takeLeft
	preferLeft
	preferRight
	takeRight
)

func (p *parser) compare(a, b int) comparison {
	pa, pb := p.s.heads[a].status == paused, p.s.heads[b].status == paused
	da, db := p.s.node(a).dynPrec, p.s.node(b).dynPrec
	switch {
	case !pa && pb:
		return takeLeft
	case pa && !pb:
		return takeRight
	case da > db:
		return preferLeft
	case db > da:
		return preferRight
	}
	return none
}

func (p *parser) checkStop() bool {
	p.ops++
	if p.ops < opsPerStopCheck {
		return true
	}
	p.ops = 0
	if p.stop != nil && p.stop() {
		p.outcome = Stopped
		return false
	}
	return true
}

func (p *parser) decline() bool {
	p.outcome = Declined
	return false
}

func (p *parser) advance(v int) bool {
	state := p.s.node(v).state
	position := p.s.node(v).position.Bytes
	external := p.s.heads[v].external
	lookahead, entry := p.cached(v, state, position)
	needsLex := lookahead == 0
	for {
		if needsLex {
			needsLex = false
			lookahead = p.lex(v, state)
			if lookahead != 0 {
				p.cacheToken, p.cacheByte, p.cacheExternal = lookahead, position, external
			}
			entry = p.t.entry(state, p.r.Trees[lookahead].Symbol)
		}
		if !p.checkStop() {
			return false
		}
		reduced := false
		lastReduction := noVersion
		for _, a := range entry.Actions {
			switch a.Type {
			case ActionShift:
				if a.Repetition {
					continue
				}
				next := a.State
				if a.Extra {
					next = state
				}
				p.shift(v, next, lookahead, a.Extra)
				return true
			case ActionReduce:
				reduced = true
				if rv := p.reduce(v, a.Symbol, uint32(a.ChildCount), int32(a.DynPrec), a.ProductionID, lookahead == 0); rv != noVersion {
					lastReduction = rv
				}
			case ActionAccept:
				p.accept(v, lookahead)
				return true
			case ActionRecover:
				return p.decline()
			}
		}
		if lastReduction != noVersion {
			p.s.renumber(lastReduction, v)
			state = p.s.node(v).state
			needsLex = lookahead == 0
			entry = p.t.entry(state, p.r.Trees[lookahead].Symbol)
			continue
		}
		if reduced {
			p.s.heads[v].status = halted
			return true
		}
		la := &p.r.Trees[lookahead]
		if la.Keyword && la.Symbol != p.t.KeywordCapture && !p.t.isReserved(state, la.Symbol) {
			if e := p.t.entry(state, p.t.KeywordCapture); len(e.Actions) > 0 {
				lookahead = p.r.copyOf(lookahead)
				p.r.Trees[lookahead].Symbol = p.t.KeywordCapture
				entry = e
				continue
			}
		}
		if state == errorState {
			return p.decline()
		}
		p.s.heads[v].status = paused
		return true
	}
}

func (p *parser) shift(v int, state uint16, lookahead uint32, extra bool) {
	if extra != p.r.Trees[lookahead].Extra {
		lookahead = p.r.copyOf(lookahead)
		p.r.Trees[lookahead].Extra = extra
	}
	p.s.push(v, lookahead, state)
	if p.r.Trees[lookahead].scan != 0 {
		p.s.heads[v].external = lookahead
	}
}

func (p *parser) splitTrailingExtras(children []uint32, into []uint32) ([]uint32, []uint32) {
	into = into[:0]
	n := len(children)
	for n > 0 && p.r.Trees[children[n-1]].Extra {
		n--
	}
	return children[:n], append(into, children[n:]...)
}

func (p *parser) reduce(v int, symbol uint16, count uint32, dynPrec int32, productionID uint16, endOfExtra bool) int {
	initial := len(p.s.heads)
	slices := p.s.pop(v, count, false)
	removed := 0
	haltedCount := p.s.haltedCount()
	for i := 0; i < len(slices); i++ {
		sl := slices[i]
		sv := sl.version - removed
		if sv > maxVersionCount+maxVersionCountOverflow+haltedCount {
			p.s.remove(sv)
			removed++
			p.s.recycle(sl.subtrees)
			for i+1 < len(slices) && slices[i+1].version == sl.version {
				i++
				p.s.recycle(slices[i].subtrees)
			}
			continue
		}
		var children []uint32
		children, p.trailing = p.splitTrailingExtras(sl.subtrees, p.trailing)
		parent := p.r.node(symbol, children, productionID)
		p.s.recycle(sl.subtrees)
		for i+1 < len(slices) && slices[i+1].version == sl.version {
			i++
			var next []uint32
			next, p.trailing2 = p.splitTrailingExtras(slices[i].subtrees, p.trailing2)
			if p.r.selectChildren(parent, next) {
				p.trailing, p.trailing2 = p.trailing2, p.trailing
				parent = p.r.node(symbol, next, productionID)
			}
			p.s.recycle(slices[i].subtrees)
		}
		state := p.s.node(sv).state
		next := p.t.nextState(state, symbol)
		p.r.Trees[parent].Extra = endOfExtra && next == state
		p.r.Trees[parent].DynPrec += dynPrec
		p.s.push(sv, parent, next)
		for _, t := range p.trailing {
			p.s.push(sv, t, next)
		}
		for j := 0; j < sv; j++ {
			if j != v && p.s.merge(j, sv) {
				removed++
				break
			}
		}
	}
	if len(p.s.heads) > initial {
		return initial
	}
	return noVersion
}

func (p *parser) accept(v int, lookahead uint32) {
	p.s.push(v, lookahead, initialState)
	slices := p.s.pop(v, 0, true)
	for _, sl := range slices {
		trees := sl.subtrees
		root := uint32(0)
		for j := len(trees) - 1; j >= 0; j-- {
			t := p.r.Trees[trees[j]]
			if t.Extra {
				continue
			}
			children := append(append(append(p.s.buffer(), trees[:j]...), p.r.Children(trees[j])...), trees[j+1:]...)
			root = p.r.node(t.Symbol, children, t.ProductionID)
			p.s.recycle(children)
			break
		}
		p.accepts++
		if p.finished == 0 || p.r.selectTree(p.finished, root) {
			p.finished = root
		}
		p.s.recycle(trees)
	}
	p.s.remove(slices[0].version)
	p.s.heads[v].status = halted
}
