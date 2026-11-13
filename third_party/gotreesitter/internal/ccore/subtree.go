package ccore

import (
	"bytes"
	"math"
)

type subtreeKind uint8

const (
	internalNode subtreeKind = iota
	leafToken
	keywordToken
	missingToken
	scanChangedToken
)

type Subtree struct {
	Padding           uint32
	Size              uint32
	kidsOrScan        uint32
	DynPrec           int32
	errorCost         uint32
	descendants       uint32
	ChildCount        uint16
	Symbol            uint16
	productionOrState uint16
	Extra             bool
	kind              subtreeKind
}

func (t *Subtree) leaf() bool { return t.kind != internalNode }

func (t *Subtree) Missing() bool { return t.kind == missingToken }

func (t *Subtree) ErrorCost() uint32 {
	if t.Missing() {
		return costPerMissingTree + costPerRecovery
	}
	return t.errorCost
}

func (t *Subtree) VisibleDescendants() uint32 { return t.descendants }

func (t *Subtree) Production() uint16 {
	if t.leaf() {
		return 0
	}
	return t.productionOrState
}

func (t *Subtree) parseState() uint16 {
	if t.leaf() {
		return t.productionOrState
	}
	return 0
}

func (t *Subtree) scan() uint32 {
	if t.leaf() {
		return t.kidsOrScan
	}
	return 0
}

type scanState struct{ start, end uint32 }

type Result struct {
	Trees     []Subtree
	Kids      []uint32
	Root      uint32
	t         *Tables
	scans     []scanState
	scanBytes []byte
	tooWide   bool
}

func (r *Result) scannerState(id uint32) []byte {
	s := r.scans[r.Trees[id].scan()]
	return r.scanBytes[s.start:s.end]
}

func (r *Result) addScan(state []byte) uint32 {
	start := uint32(len(r.scanBytes))
	r.scanBytes = append(r.scanBytes, state...)
	r.scans = append(r.scans, scanState{start, uint32(len(r.scanBytes))})
	return uint32(len(r.scans) - 1)
}

func (r *Result) sameScannerState(a, b uint32) bool {
	return r.Trees[a].scan() == r.Trees[b].scan() || bytes.Equal(r.scannerState(a), r.scannerState(b))
}

func (r *Result) Children(id uint32) []uint32 {
	t := &r.Trees[id]
	if t.leaf() {
		return nil
	}
	return r.Kids[t.kidsOrScan : t.kidsOrScan+uint32(t.ChildCount)]
}

func (t *Subtree) Total() uint32 { return t.Padding + t.Size }

func (r *Result) leaf(symbol uint16, padding, size uint32, parseState uint16, kind subtreeKind) uint32 {
	r.Trees = append(r.Trees, Subtree{
		Padding:           padding,
		Size:              size,
		Symbol:            symbol,
		productionOrState: parseState,
		Extra:             symbol == symEnd,
		kind:              kind,
	})
	return uint32(len(r.Trees) - 1)
}

func (r *Result) copyOf(id uint32) uint32 {
	r.Trees = append(r.Trees, r.Trees[id])
	return uint32(len(r.Trees) - 1)
}

func (r *Result) shown(child *Subtree, aliases []uint16, structural int) bool {
	aliased := !child.Extra && child.Symbol != 0 && structural < len(aliases) && aliases[structural] != 0
	return aliased || r.t.Visible[child.Symbol]
}

func (r *Result) node(symbol uint16, children []uint32, productionID uint16) uint32 {
	if len(children) > math.MaxUint16 {
		r.tooWide = true
	}
	n := Subtree{Symbol: symbol, productionOrState: productionID, kidsOrScan: uint32(len(r.Kids)), ChildCount: uint16(len(children))}
	aliases := r.t.aliases(productionID)
	structural := 0
	for i, c := range children {
		ct := &r.Trees[c]
		if i == 0 {
			n.Padding, n.Size = ct.Padding, ct.Size
		} else {
			n.Size += ct.Total()
		}
		n.DynPrec += ct.DynPrec
		n.errorCost += ct.ErrorCost()
		n.descendants += ct.descendants
		if r.shown(ct, aliases, structural) {
			n.descendants++
		}
		if !ct.Extra {
			structural++
		}
	}
	r.Kids = append(r.Kids, children...)
	r.Trees = append(r.Trees, n)
	return uint32(len(r.Trees) - 1)
}

func (r *Result) visibleChildCount(id uint32) uint32 {
	count := uint32(0)
	aliases := r.t.aliases(r.Trees[id].Production())
	structural := 0
	for _, c := range r.Children(id) {
		ct := &r.Trees[c]
		switch {
		case r.shown(ct, aliases, structural):
			count++
		case ct.ChildCount > 0:
			count += r.visibleChildCount(c)
		}
		if !ct.Extra {
			structural++
		}
	}
	return count
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

func rightWins(leftCost, rightCost uint32, leftDyn, rightDyn int32) (pick, decided bool) {
	switch {
	case leftCost != rightCost:
		return rightCost < leftCost, true
	case leftDyn != rightDyn:
		return rightDyn > leftDyn, true
	}
	return true, leftCost > 0
}

func (r *Result) selectTree(left, right uint32) bool {
	l, rt := &r.Trees[left], &r.Trees[right]
	if pick, decided := rightWins(l.ErrorCost(), rt.ErrorCost(), l.DynPrec, rt.DynPrec); decided {
		return pick
	}
	return r.compare(left, right) == 1
}

func (r *Result) selectChildren(left uint32, children []uint32) bool {
	dyn, cost := int32(0), uint32(0)
	for _, c := range children {
		dyn += r.Trees[c].DynPrec
		cost += r.Trees[c].ErrorCost()
	}
	l := &r.Trees[left]
	if pick, decided := rightWins(l.ErrorCost(), cost, l.DynPrec, dyn); decided {
		return pick
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
	if l.Symbol != rt.Symbol {
		return false
	}
	if l.ErrorCost() > 0 && rt.ErrorCost() > 0 {
		return true
	}
	return l.Padding == rt.Padding && l.Size == rt.Size && l.ChildCount == rt.ChildCount && l.Extra == rt.Extra
}
