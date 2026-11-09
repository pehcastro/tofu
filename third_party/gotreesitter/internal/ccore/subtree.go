package ccore

import "bytes"

type Subtree struct {
	Padding      Length
	Size         Length
	ChildStart   uint32
	ChildCount   uint32
	DynPrec      int32
	scan         uint32
	Symbol       uint16
	ParseState   uint16
	ProductionID uint16
	Extra        bool
	Keyword      bool
}

type scanState struct{ start, end uint32 }

type Result struct {
	Trees     []Subtree
	Kids      []uint32
	Root      uint32
	scans     []scanState
	scanBytes []byte
}

func (r *Result) scannerState(id uint32) []byte {
	s := r.scans[r.Trees[id].scan]
	return r.scanBytes[s.start:s.end]
}

func (r *Result) addScan(state []byte) uint32 {
	start := uint32(len(r.scanBytes))
	r.scanBytes = append(r.scanBytes, state...)
	r.scans = append(r.scans, scanState{start, uint32(len(r.scanBytes))})
	return uint32(len(r.scans) - 1)
}

func (r *Result) sameScannerState(a, b uint32) bool {
	return r.Trees[a].scan == r.Trees[b].scan || bytes.Equal(r.scannerState(a), r.scannerState(b))
}

func (r *Result) Children(id uint32) []uint32 {
	t := &r.Trees[id]
	return r.Kids[t.ChildStart : t.ChildStart+t.ChildCount]
}

func (t *Subtree) Total() Length { return t.Padding.Add(t.Size) }

func (r *Result) leaf(symbol uint16, padding, size Length, parseState uint16, keyword bool) uint32 {
	r.Trees = append(r.Trees, Subtree{
		Padding:    padding,
		Size:       size,
		Symbol:     symbol,
		ParseState: parseState,
		Extra:      symbol == symEnd,
		Keyword:    keyword,
	})
	return uint32(len(r.Trees) - 1)
}

func (r *Result) copyOf(id uint32) uint32 {
	r.Trees = append(r.Trees, r.Trees[id])
	return uint32(len(r.Trees) - 1)
}

func (r *Result) node(symbol uint16, children []uint32, productionID uint16) uint32 {
	n := Subtree{Symbol: symbol, ProductionID: productionID, ChildStart: uint32(len(r.Kids)), ChildCount: uint32(len(children))}
	for i, c := range children {
		ct := &r.Trees[c]
		if i == 0 {
			n.Padding, n.Size = ct.Padding, ct.Size
		} else {
			n.Size = n.Size.Add(ct.Total())
		}
		n.DynPrec += ct.DynPrec
	}
	r.Kids = append(r.Kids, children...)
	r.Trees = append(r.Trees, n)
	return uint32(len(r.Trees) - 1)
}

func (r *Result) compare(left, right uint32) int {
	l, rt := &r.Trees[left], &r.Trees[right]
	if l.Symbol != rt.Symbol {
		return order(l.Symbol < rt.Symbol)
	}
	return r.compareKids(r.Children(left), r.Children(right))
}

func (r *Result) compareKids(left, right []uint32) int {
	if len(left) != len(right) {
		return order(len(left) < len(right))
	}
	for i := range left {
		if c := r.compare(left[i], right[i]); c != 0 {
			return c
		}
	}
	return 0
}

func order(less bool) int {
	if less {
		return -1
	}
	return 1
}

func (r *Result) selectTree(left, right uint32) bool {
	if l, rt := r.Trees[left].DynPrec, r.Trees[right].DynPrec; l != rt {
		return rt > l
	}
	return r.compare(left, right) == 1
}

func (r *Result) selectChildren(left uint32, children []uint32) bool {
	dyn := int32(0)
	for _, c := range children {
		dyn += r.Trees[c].DynPrec
	}
	if l := r.Trees[left].DynPrec; l != dyn {
		return dyn > l
	}
	return r.compareKids(r.Children(left), children) == 1
}

func (r *Result) equivalent(left, right uint32) bool {
	if left == right {
		return true
	}
	if left == 0 || right == 0 {
		return false
	}
	l, rt := &r.Trees[left], &r.Trees[right]
	return l.Symbol == rt.Symbol && l.Padding.Bytes == rt.Padding.Bytes && l.Size.Bytes == rt.Size.Bytes &&
		l.ChildCount == rt.ChildCount && l.Extra == rt.Extra
}
