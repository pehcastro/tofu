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
	position  Length
	refs      uint32
	dynPrec   int32
	state     uint16
	linkCount uint8
}

type status uint8

const (
	active status = iota
	paused
	halted
)

type head struct {
	node     uint32
	external uint32
	status   status
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
		n.position, n.dynPrec = p.position, p.dynPrec
		if subtree != 0 {
			t := &s.r.Trees[subtree]
			n.position = n.position.Add(t.Total())
			n.dynPrec += t.DynPrec
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
		if en.state == ln.state && en.position.Bytes == ln.position.Bytes {
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
	if dyn := s.nodes[l.node].dynPrec + s.r.Trees[l.subtree].DynPrec; dyn > n.dynPrec {
		n.dynPrec = dyn
	}
}

func (s *stack) node(v int) *stackNode { return &s.nodes[s.heads[v].node] }

func (s *stack) push(v int, subtree uint32, state uint16) {
	s.heads[v].node = s.newNode(s.heads[v].node, subtree, state)
}

func (s *stack) addVersion(original int, node uint32) int {
	s.heads = append(s.heads, head{node: node, external: s.heads[original].external})
	s.nodes[node].refs++
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
	s.release(s.heads[to].node)
	s.heads[to] = s.heads[from]
	s.heads = append(s.heads[:from], s.heads[from+1:]...)
}

func (s *stack) swap(a, b int) { s.heads[a], s.heads[b] = s.heads[b], s.heads[a] }

func (s *stack) canMerge(a, b int) bool {
	ha, hb := s.heads[a], s.heads[b]
	na, nb := &s.nodes[ha.node], &s.nodes[hb.node]
	return ha.status == active && hb.status == active && na.state == nb.state && na.position.Bytes == nb.position.Bytes &&
		s.r.sameScannerState(ha.external, hb.external)
}

func (s *stack) merge(a, b int) bool {
	if !s.canMerge(a, b) {
		return false
	}
	target, from := s.heads[a].node, s.heads[b].node
	for i := 0; i < int(s.nodes[from].linkCount); i++ {
		s.addLink(target, s.nodes[from].links[i])
	}
	s.remove(b)
	return true
}

func (s *stack) haltedCount() int {
	n := 0
	for _, h := range s.heads {
		if h.status == halted {
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

func (s *stack) pop(v int, goal uint32, all bool) []slice {
	s.slices = s.slices[:0]
	s.iterators = append(s.iterators[:0], iterator{node: s.heads[v].node, subtrees: s.buffer()})
	for len(s.iterators) > 0 {
		for i, size := 0, len(s.iterators); i < size; i++ {
			it := &s.iterators[i]
			node := &s.nodes[it.node]
			reached := it.count == goal && !all
			if reached || node.linkCount == 0 {
				if reached || all {
					subtrees := it.subtrees
					for a, b := 0, len(subtrees)-1; a < b; a, b = a+1, b-1 {
						subtrees[a], subtrees[b] = subtrees[b], subtrees[a]
					}
					s.addSlice(v, it.node, subtrees)
				} else {
					s.recycle(it.subtrees)
				}
				s.iterators = append(s.iterators[:i], s.iterators[i+1:]...)
				i--
				size--
				continue
			}
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
					cur.subtrees = s.cloned(cur.subtrees)
					s.iterators = append(s.iterators, cur)
					next = &s.iterators[len(s.iterators)-1]
				}
				next.node = l.node
				if l.subtree == 0 {
					next.count++
					continue
				}
				next.subtrees = append(next.subtrees, l.subtree)
				if !s.r.Trees[l.subtree].Extra {
					next.count++
				}
			}
		}
	}
	return s.slices
}
