package uv

import "hash/maphash"

func hash(h *maphash.Hash, l Line) uint64 {
	h.Reset()
	for _, c := range l {

		_, _ = h.WriteString(c.Content)
	}

	return h.Sum64()
}

type hashmap struct {
	value              uint64
	oldcount, newcount int
	oldindex, newindex int
}

const newIndex = -1

func (s *TerminalRenderer) updateHashmap(newbuf *RenderBuffer) {
	height := newbuf.Height()

	if len(s.oldhash) >= height && len(s.newhash) >= height {

		for i := range height {
			if newbuf.Touched == nil || newbuf.Touched[i] != nil {

				s.oldhash[i] = hash(&s.hasher, s.curbuf.Line(i))
				s.newhash[i] = hash(&s.hasher, newbuf.Line(i))
			}
		}
	} else {

		if len(s.oldhash) != height {
			s.oldhash = make([]uint64, height)
		}
		if len(s.newhash) != height {
			s.newhash = make([]uint64, height)
		}
		for i := range height {
			s.oldhash[i] = hash(&s.hasher, s.curbuf.Line(i))
			s.newhash[i] = hash(&s.hasher, newbuf.Line(i))
		}
	}

	s.hashtab = make([]hashmap, (height+1)*2)
	for i := range height {
		hashval := s.oldhash[i]

		var idx int
		for idx = 0; idx < len(s.hashtab) && s.hashtab[idx].value != 0; idx++ {
			if s.hashtab[idx].value == hashval {
				break
			}
		}

		s.hashtab[idx].value = hashval
		s.hashtab[idx].oldcount++
		s.hashtab[idx].oldindex = i
	}
	for i := range height {
		hashval := s.newhash[i]

		var idx int
		for idx = 0; idx < len(s.hashtab) && s.hashtab[idx].value != 0; idx++ {
			if s.hashtab[idx].value == hashval {
				break
			}
		}

		s.hashtab[idx].value = hashval
		s.hashtab[idx].newcount++
		s.hashtab[idx].newindex = i
		s.oldnum[i] = newIndex
	}

	for i := 0; i < len(s.hashtab) && s.hashtab[i].value != 0; i++ {
		hsp := &s.hashtab[i]
		if hsp.oldcount == 1 && hsp.newcount == 1 && hsp.oldindex != hsp.newindex {
			s.oldnum[hsp.newindex] = hsp.oldindex
		}
	}

	s.growHunks(newbuf)

	for i := 0; i < height; {
		var start, shift, size int
		for i < height && s.oldnum[i] == newIndex {
			i++
		}
		if i >= height {
			break
		}
		start = i
		shift = s.oldnum[i] - i
		i++
		for i < height && s.oldnum[i] != newIndex && s.oldnum[i]-i == shift {
			i++
		}
		size = i - start
		if size < 3 || size+min(size/8, 2) < abs(shift) {
			for start < i {
				s.oldnum[start] = newIndex
				start++
			}
		}
	}

	s.growHunks(newbuf)
}

func (s *TerminalRenderer) scrollOldhash(n, top, bot int) {
	if len(s.oldhash) == 0 {
		return
	}

	size := bot - top + 1 - abs(n)
	if n > 0 {

		copy(s.oldhash[top:], s.oldhash[top+n:top+n+size])

		for i := bot; i > bot-n; i-- {
			s.oldhash[i] = hash(&s.hasher, s.curbuf.Line(i))
		}
	} else {

		copy(s.oldhash[top-n:], s.oldhash[top:top+size])

		for i := top; i < top-n; i++ {
			s.oldhash[i] = hash(&s.hasher, s.curbuf.Line(i))
		}
	}
}

func (s *TerminalRenderer) growHunks(newbuf *RenderBuffer) {
	var (
		backLimit    int
		backRefLimit int
		i            int
		nextHunk     int
	)

	height := newbuf.Height()
	for i < height && s.oldnum[i] == newIndex {
		i++
	}
	for ; i < height; i = nextHunk {
		var (
			forwardLimit    int
			forwardRefLimit int
			end             int
			start           = i
			shift           = s.oldnum[i] - i
		)

		i = start + 1
		for i < height &&
			s.oldnum[i] != newIndex &&
			s.oldnum[i]-i == shift {
			i++
		}

		end = i
		for i < height && s.oldnum[i] == newIndex {
			i++
		}

		nextHunk = i
		forwardLimit = i
		if i >= height || s.oldnum[i] >= i {
			forwardRefLimit = i
		} else {
			forwardRefLimit = s.oldnum[i]
		}

		i = start - 1

		if shift < 0 {
			backLimit = backRefLimit + (-shift)
		}
		for i >= backLimit {
			if s.newhash[i] == s.oldhash[i+shift] ||
				s.costEffective(newbuf, i+shift, i, shift < 0) {
				s.oldnum[i] = i + shift
			} else {
				break
			}
			i--
		}

		i = end

		if shift > 0 {
			forwardLimit = forwardRefLimit - shift
		}
		for i < forwardLimit {
			if s.newhash[i] == s.oldhash[i+shift] ||
				s.costEffective(newbuf, i+shift, i, shift > 0) {
				s.oldnum[i] = i + shift
			} else {
				break
			}
			i++
		}

		backLimit = i
		backRefLimit = backLimit
		if shift > 0 {
			backRefLimit += shift
		}
	}
}

func (s *TerminalRenderer) costEffective(newbuf *RenderBuffer, from, to int, blank bool) bool {
	if from == to {
		return false
	}

	newFrom := s.oldnum[from]
	if newFrom == newIndex {
		newFrom = from
	}

	var costBeforeMove int
	if blank {

		costBeforeMove = s.updateCostBlank(newbuf, newbuf.Line(to))
	} else {

		costBeforeMove = s.updateCost(newbuf, s.curbuf.Line(to), newbuf.Line(to))
	}

	costBeforeMove += s.updateCost(newbuf, s.curbuf.Line(newFrom), newbuf.Line(from))

	var costAfterMove int
	if newFrom == from {

		costAfterMove = s.updateCostBlank(newbuf, newbuf.Line(from))
	} else {

		costAfterMove = s.updateCost(newbuf, s.curbuf.Line(newFrom), newbuf.Line(from))
	}

	costAfterMove += s.updateCost(newbuf, s.curbuf.Line(from), newbuf.Line(to))

	return costBeforeMove >= costAfterMove
}

func (s *TerminalRenderer) updateCost(_ *RenderBuffer, from, to Line) (cost int) {
	var fidx, tidx int
	for i := s.curbuf.Width(); i > 0; i, fidx, tidx = i-1, fidx+1, tidx+1 {
		if !cellEqual(from.At(fidx), to.At(tidx)) {
			cost++
		}
	}
	return
}

func (s *TerminalRenderer) updateCostBlank(_ *RenderBuffer, to Line) (cost int) {

	blank := s.clearBlank()
	var tidx int
	for i := s.curbuf.Width(); i > 0; i, tidx = i-1, tidx+1 {
		if !cellEqual(blank, to.At(tidx)) {
			cost++
		}
	}
	return
}
