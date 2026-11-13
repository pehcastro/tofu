package ccore

const (
	maxLinkCount     = 8
	maxIteratorCount = 64
	noVersion        = -1
)

type link struct {
	node    uint32
	subtree uint32
}

type stackNode struct {
	links     [maxLinkCount]link
	position  uint32
	refs      uint32
	dynPrec   int32
	errorCost uint32
	nodeCount uint32
	state     uint16
	linkCount uint8
}

type status uint8

const (
	active status = iota
	paused
	halted
)

type summaryEntry struct {
	position uint32
	depth    uint32
	state    uint16
}

type head struct {
	node                 uint32
	external             uint32
	lookahead            uint32
	nodeCountAtLastError uint32
	summary              uint32
	status               status
}

type iterator struct {
	node     uint32
	subtrees []uint32
	count    uint32
}

type slice struct {
	subtrees []uint32
	version  int
}

type stack struct {
	r         *Result
	nodes     []stackNode
	free      []uint32
	heads     []head
	iterators []iterator
	slices    []slice
	buffers   [][]uint32
	summaries [][]summaryEntry
}

func (s *stack) subtreeNodeCount(id uint32) uint32 {
	t := &s.r.Trees[id]
	count := t.descendants
	if s.r.t.Visible[t.Symbol] {
		count++
	}
	if t.Symbol == s.r.t.errorRepeatSymbol() {
		count++
	}
	return count
}

func (s *stack) newNode(prev, subtree uint32, state uint16) uint32 {
	var id uint32
	if n := len(s.free); n > 0 {
		id = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		s.nodes = append(s.nodes, stackNode{})
		id = uint32(len(s.nodes) - 1)
	}
	n := &s.nodes[id]
	*n = stackNode{refs: 1, state: state}
	if prev != 0 {
		p := &s.nodes[prev]
		n.linkCount = 1
		n.links[0] = link{node: prev, subtree: subtree}
		n.position, n.dynPrec, n.errorCost, n.nodeCount = p.position, p.dynPrec, p.errorCost, p.nodeCount
		if subtree != 0 {
			t := &s.r.Trees[subtree]
			n.position += t.Total()
			n.dynPrec += t.DynPrec
			n.errorCost += t.ErrorCost()
			n.nodeCount += s.subtreeNodeCount(subtree)
		}
	}
	return id
}

func (s *stack) release(id uint32) {
	for id != 0 {
		n := &s.nodes[id]
		n.refs--
		if n.refs > 0 {
			return
		}
		first := uint32(0)
		if n.linkCount > 0 {
			for i := n.linkCount - 1; i > 0; i-- {
				s.release(s.nodes[id].links[i].node)
			}
			first = s.nodes[id].links[0].node
		}
		s.free = append(s.free, id)
		id = first
	}
}

func (s *stack) addLink(self uint32, l link) {
	if l.node == self {
		return
	}
	for i := 0; i < int(s.nodes[self].linkCount); i++ {
		existing := s.nodes[self].links[i]
		if !s.r.equivalent(existing.subtree, l.subtree) {
			continue
		}
		if existing.node == l.node {
			if s.r.Trees[l.subtree].DynPrec > s.r.Trees[existing.subtree].DynPrec {
				s.nodes[self].links[i].subtree = l.subtree
				s.nodes[self].dynPrec = s.nodes[l.node].dynPrec + s.r.Trees[l.subtree].DynPrec
			}
			return
		}
		en, ln := &s.nodes[existing.node], &s.nodes[l.node]
		if en.state == ln.state && en.position == ln.position && en.errorCost == ln.errorCost {
			for j := 0; j < int(s.nodes[l.node].linkCount); j++ {
				s.addLink(existing.node, s.nodes[l.node].links[j])
			}
			dyn := s.nodes[l.node].dynPrec + s.r.Trees[l.subtree].DynPrec
			if dyn > s.nodes[self].dynPrec {
				s.nodes[self].dynPrec = dyn
			}
			return
		}
	}
	n := &s.nodes[self]
	if n.linkCount == maxLinkCount {
		return
	}
	s.nodes[l.node].refs++
	n.links[n.linkCount] = l
	n.linkCount++
	count, dyn := s.nodes[l.node].nodeCount, s.nodes[l.node].dynPrec
	if l.subtree != 0 {
		count += s.subtreeNodeCount(l.subtree)
		dyn += s.r.Trees[l.subtree].DynPrec
	}
	n.nodeCount = max(n.nodeCount, count)
	n.dynPrec = max(n.dynPrec, dyn)
}

func (s *stack) node(v int) *stackNode { return &s.nodes[s.heads[v].node] }

func (s *stack) push(v int, subtree uint32, state uint16) {
	h := &s.heads[v]
	h.node = s.newNode(h.node, subtree, state)
	if subtree == 0 {
		h.nodeCountAtLastError = s.nodes[h.node].nodeCount
	}
}

func (s *stack) errorCost(v int) uint32 {
	n := s.node(v)
	if s.heads[v].status == paused || n.state == errorState && n.links[0].subtree == 0 {
		return n.errorCost + costPerRecovery
	}
	return n.errorCost
}

func (s *stack) nodeCountSinceError(v int) uint32 {
	h := &s.heads[v]
	count := s.nodes[h.node].nodeCount
	h.nodeCountAtLastError = min(h.nodeCountAtLastError, count)
	return count - h.nodeCountAtLastError
}

func (s *stack) hasAdvancedSinceError(v int) bool {
	h := &s.heads[v]
	n := &s.nodes[h.node]
	if n.errorCost == 0 {
		return true
	}
	for n.linkCount > 0 && n.links[0].subtree != 0 {
		t := &s.r.Trees[n.links[0].subtree]
		if t.Total() > 0 {
			return true
		}
		if n.nodeCount <= h.nodeCountAtLastError || t.ErrorCost() != 0 {
			return false
		}
		n = &s.nodes[n.links[0].node]
	}
	return false
}

func (s *stack) pause(v int, lookahead uint32) {
	h := &s.heads[v]
	h.status, h.lookahead, h.nodeCountAtLastError = paused, lookahead, s.nodes[h.node].nodeCount
}

func (s *stack) resume(v int) uint32 {
	h := &s.heads[v]
	lookahead := h.lookahead
	h.status, h.lookahead = active, 0
	return lookahead
}

func (s *stack) addVersion(original int, node uint32) int {
	s.heads = append(s.heads, head{})
	o, h := &s.heads[original], &s.heads[len(s.heads)-1]
	h.node, h.external, h.nodeCountAtLastError = node, o.external, o.nodeCountAtLastError
	s.nodes[node].refs++
	return len(s.heads) - 1
}

func (s *stack) copyVersion(v int) int {
	s.heads = append(s.heads, s.heads[v])
	h := &s.heads[len(s.heads)-1]
	h.summary = 0
	s.nodes[h.node].refs++
	return len(s.heads) - 1
}

func (s *stack) remove(v int) {
	s.release(s.heads[v].node)
	s.heads = append(s.heads[:v], s.heads[v+1:]...)
}

func (s *stack) renumber(from, to int) {
	if from == to {
		return
	}
	summary := s.heads[to].summary
	s.release(s.heads[to].node)
	s.heads[to] = s.heads[from]
	if s.heads[to].summary == 0 {
		s.heads[to].summary = summary
	}
	s.heads = append(s.heads[:from], s.heads[from+1:]...)
}

func (s *stack) swap(a, b int) { s.heads[a], s.heads[b] = s.heads[b], s.heads[a] }

func (s *stack) canMerge(a, b int) bool {
	ha, hb := &s.heads[a], &s.heads[b]
	na, nb := &s.nodes[ha.node], &s.nodes[hb.node]
	return ha.status == active && hb.status == active && na.state == nb.state && na.position == nb.position &&
		na.errorCost == nb.errorCost && s.r.sameScannerState(ha.external, hb.external)
}

func (s *stack) merge(a, b int) bool {
	if !s.canMerge(a, b) {
		return false
	}
	target, from := s.heads[a].node, s.heads[b].node
	for i := 0; i < int(s.nodes[from].linkCount); i++ {
		s.addLink(target, s.nodes[from].links[i])
	}
	if s.nodes[target].state == errorState {
		s.heads[a].nodeCountAtLastError = s.nodes[target].nodeCount
	}
	s.remove(b)
	return true
}

func (s *stack) haltedCount() int {
	n := 0
	for i := range s.heads {
		if s.heads[i].status == halted {
			n++
		}
	}
	return n
}

func (s *stack) buffer() []uint32 {
	if n := len(s.buffers); n > 0 {
		b := s.buffers[n-1]
		s.buffers = s.buffers[:n-1]
		return b[:0]
	}
	return nil
}

func (s *stack) recycle(b []uint32) { s.buffers = append(s.buffers, b) }

func (s *stack) cloned(b []uint32) []uint32 { return append(s.buffer(), b...) }

func (s *stack) addSlice(original int, node uint32, subtrees []uint32) {
	for i := len(s.slices) - 1; i >= 0; i-- {
		if s.heads[s.slices[i].version].node == node {
			s.slices = append(s.slices, slice{})
			copy(s.slices[i+2:], s.slices[i+1:])
			s.slices[i+1] = slice{subtrees: subtrees, version: s.slices[i].version}
			return
		}
	}
	s.slices = append(s.slices, slice{subtrees: subtrees, version: s.addVersion(original, node)})
}

func (s *stack) follow(i int, collect bool) {
	node := &s.nodes[s.iterators[i].node]
	for j := 1; j <= int(node.linkCount); j++ {
		var l link
		var next *iterator
		if j == int(node.linkCount) {
			l = node.links[0]
			next = &s.iterators[i]
		} else {
			if len(s.iterators) >= maxIteratorCount {
				continue
			}
			l = node.links[j]
			cur := s.iterators[i]
			if collect {
				cur.subtrees = s.cloned(cur.subtrees)
			}
			s.iterators = append(s.iterators, cur)
			next = &s.iterators[len(s.iterators)-1]
		}
		next.node = l.node
		if l.subtree == 0 {
			next.count++
			continue
		}
		if collect {
			next.subtrees = append(next.subtrees, l.subtree)
		}
		if !s.r.Trees[l.subtree].Extra {
			next.count++
		}
	}
}

func (s *stack) drop(i int) { s.iterators = append(s.iterators[:i], s.iterators[i+1:]...) }

func (s *stack) pop(v int, goal uint32, all bool) []slice {
	s.slices = s.slices[:0]
	s.iterators = append(s.iterators[:0], iterator{})
	s.iterators[0].node, s.iterators[0].subtrees = s.heads[v].node, s.buffer()
	for len(s.iterators) > 0 {
		for i, size := 0, len(s.iterators); i < size; i++ {
			it := &s.iterators[i]
			reached := it.count == goal && !all
			if reached || s.nodes[it.node].linkCount == 0 {
				if reached || all {
					subtrees := it.subtrees
					for a, b := 0, len(subtrees)-1; a < b; a, b = a+1, b-1 {
						subtrees[a], subtrees[b] = subtrees[b], subtrees[a]
					}
					s.addSlice(v, it.node, subtrees)
				} else {
					s.recycle(it.subtrees)
				}
				s.drop(i)
				i--
				size--
				continue
			}
			s.follow(i, true)
		}
	}
	return s.slices
}

func (s *stack) summarize(v int, maxDepth uint32) {
	if s.heads[v].summary == 0 {
		s.heads[v].summary = uint32(len(s.summaries))
		s.summaries = append(s.summaries, nil)
	}
	summary := s.summaries[s.heads[v].summary][:0]
	s.iterators = append(s.iterators[:0], iterator{node: s.heads[v].node})
	for len(s.iterators) > 0 {
		for i, size := 0, len(s.iterators); i < size; i++ {
			it := &s.iterators[i]
			node := &s.nodes[it.node]
			if it.count > maxDepth {
				s.drop(i)
				i--
				size--
				continue
			}
			seen := false
			for k := len(summary) - 1; k >= 0 && summary[k].depth >= it.count; k-- {
				if summary[k].depth == it.count && summary[k].state == node.state {
					seen = true
					break
				}
			}
			if !seen {
				summary = append(summary, summaryEntry{position: node.position, depth: it.count, state: node.state})
			}
			if node.linkCount == 0 {
				s.drop(i)
				i--
				size--
				continue
			}
			s.follow(i, false)
		}
	}
	s.summaries[s.heads[v].summary] = summary
}

func (s *stack) popError(v int) uint32 {
	h := &s.heads[v]
	n := &s.nodes[h.node]
	for _, l := range n.links[:n.linkCount] {
		if l.subtree != 0 && s.r.Trees[l.subtree].Symbol == s.r.t.errorSymbol() {
			s.nodes[l.node].refs++
			s.release(h.node)
			h.node, h.status, h.lookahead = l.node, active, 0
			return l.subtree
		}
	}
	return 0
}
