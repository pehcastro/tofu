package gotreesitter

import (
	"slices"

	"github.com/odvcencio/gotreesitter/internal/ccore"
)

type ccoreBuilder struct {
	b        *ccoreBinding
	r        *ccore.Result
	arena    *nodeArena
	nodes    []Node
	kids     []*Node
	fields   []FieldID
	lines    []uint32
	row      int
	unmapped bool
}

func lineStarts(source []byte) []uint32 {
	lines := []uint32{0}
	for at, c := range source {
		if c == '\n' {
			lines = append(lines, uint32(at+1))
		}
	}
	return lines
}

func (m *ccoreBuilder) points(start, end uint32) (Point, Point) {
	for m.row+1 < len(m.lines) && m.lines[m.row+1] <= start {
		m.row++
	}
	after, found := slices.BinarySearch(m.lines[m.row:], end)
	row := m.row + after
	if !found {
		row--
	}
	return Point{Row: uint32(m.row), Column: start - m.lines[m.row]}, Point{Row: uint32(row), Column: end - m.lines[row]}
}

func (b *ccoreBinding) materialize(r *ccore.Result, source []byte) (*Tree, ccore.Status) {
	bound := 1 + int(r.Trees[r.Root].VisibleDescendants())
	arena := acquireNodeArena(arenaClassFull)
	arena.ensureNodeCapacity(bound)
	arena.used = bound
	m := &ccoreBuilder{b: b, r: r, arena: arena, nodes: arena.nodes[:1:bound], kids: arena.allocNodeSliceNoClear(bound)[:0], fields: arena.allocFieldIDSlice(bound), lines: lineStarts(source)}
	root := &m.nodes[0]
	m.build(root, r.Root, r.Trees[r.Root].Symbol, 0)
	arena.used = len(m.nodes)
	if m.unmapped {
		arena.Release()
		return nil, ccore.UnmappedSymbol
	}
	tree := NewTree(root, source, b.target)
	tree.arena = arena
	tree.incrementalReuseDisabled = true
	tree.incrementalReuseUnsupportedClause = newCoreIncrementalReuseClause
	return tree, ccore.Accepted
}

func (b *ccoreBinding) childSymbol(r *ccore.Result, parent uint32, structural int, child *ccore.Subtree) uint16 {
	if child.Extra {
		return child.Symbol
	}
	if row := b.source.AliasSequences[r.Trees[parent].Production()]; structural < len(row) && row[structural] != 0 {
		return uint16(row[structural])
	}
	return child.Symbol
}

func (b *ccoreBinding) visibleChildren(r *ccore.Result, id uint32) int {
	n, structural := 0, 0
	for _, c := range r.Children(id) {
		t := &r.Trees[c]
		if b.meta[b.childSymbol(r, id, structural, t)].Visible {
			n++
		} else {
			n += b.visibleChildren(r, c)
		}
		if !t.Extra {
			structural++
		}
	}
	return n
}

func (b *ccoreBinding) directField(production uint16, structural int) FieldID {
	span := b.source.FieldMapSlices[production]
	for _, e := range b.source.FieldMapEntries[span[0] : span[0]+span[1]] {
		if !e.Inherited && int(e.ChildIndex) == structural {
			return e.FieldID
		}
	}
	return 0
}

func (m *ccoreBuilder) build(n *Node, id uint32, symbol uint16, pos uint32) {
	t := &m.r.Trees[id]
	start := pos + t.Padding
	end := start + t.Size
	if !m.b.mapped[symbol] {
		m.unmapped = true
	}
	n.ownerArena = m.arena
	n.symbol = m.b.symbols[symbol]
	n.setNamed(m.b.meta[symbol].Named)
	n.setExtra(t.Extra)
	n.setMissing(t.Missing())
	n.setHasError(t.ErrorCost() > 0)
	n.startByte, n.endByte = start, end
	n.startPoint, n.endPoint = m.points(start, end)
	n.childIndex = -1
	count := m.b.visibleChildren(m.r, id)
	if count == 0 {
		return
	}
	first := len(m.kids)
	m.kids = m.kids[:first+count]
	kids, fields := m.kids[first:first+count], m.fields[first:first+count]
	k := 0
	m.fill(n, id, kids, fields, &k, 0, 0, pos)
	n.children = kids
	for _, f := range fields {
		if f != 0 {
			n.fieldMetadata = m.arena.allocNodeFieldMetadata()
			n.fieldMetadata.ids = fields
			break
		}
	}
}

func (m *ccoreBuilder) fill(parent *Node, id uint32, kids []*Node, fields []FieldID, k *int, inherited FieldID, supertypes uint32, running uint32) {
	structural := 0
	production := m.r.Trees[id].Production()
	for _, c := range m.r.Children(id) {
		t := &m.r.Trees[c]
		symbol := m.b.childSymbol(m.r, id, structural, t)
		field := FieldID(0)
		if !t.Extra {
			field = m.b.directField(production, structural)
			structural++
			if field == 0 {
				field = inherited
			}
		}
		if m.b.meta[symbol].Visible {
			m.nodes = m.nodes[:len(m.nodes)+1]
			child := &m.nodes[len(m.nodes)-1]
			kids[*k], fields[*k] = child, m.b.fields[field]
			child.parent = parent
			m.build(child, c, symbol, running)
			child.childIndex = int32(*k)
			if supertypes != 0 {
				m.arena.setNodeSupertypeMask(child, supertypes)
			}
			*k++
		} else {
			m.fill(parent, c, kids, fields, k, field, supertypes|m.b.supertypes[symbol], running)
		}
		running += t.Total()
	}
}

type FlatFlags uint8

const (
	FlatNamed FlatFlags = 1 << iota
	FlatExtra
)

type FlatNode struct {
	Start, End     uint32
	Parent         uint32
	Kids, KidCount uint32
	Symbol         Symbol
	Field          FieldID
	Flags          FlatFlags
}

func (n *FlatNode) IsError() bool { return n.Symbol == errorSymbol }

func (n *FlatNode) Type(lang *Language) string {
	if n.IsError() {
		return "ERROR"
	}
	if int(n.Symbol) < len(lang.SymbolNames) {
		return unescapePunctuationSymbolName(lang.SymbolNames[n.Symbol])
	}
	return ""
}

type flatBuilder struct {
	b     *ccoreBinding
	r     *ccore.Result
	nodes []FlatNode
}

func (b *ccoreBinding) flatten(r *ccore.Result, into []FlatNode) ([]FlatNode, bool) {
	f := &flatBuilder{b: b, r: r, nodes: append(slices.Grow(into[:0], 1+int(r.Trees[r.Root].VisibleDescendants())), FlatNode{})}
	ok := f.node(0, r.Root, r.Trees[r.Root].Symbol, 0)
	return f.nodes, ok
}

func (f *flatBuilder) node(at, id uint32, symbol uint16, pos uint32) bool {
	t := &f.r.Trees[id]
	n := &f.nodes[at]
	n.Start = pos + t.Padding
	n.End = n.Start + t.Size
	n.Symbol = f.b.symbols[symbol]
	if f.b.meta[symbol].Named {
		n.Flags |= FlatNamed
	}
	if t.Extra {
		n.Flags |= FlatExtra
	}
	n.Kids, n.KidCount = uint32(len(f.nodes)), uint32(f.b.visibleChildren(f.r, id))
	f.nodes = f.nodes[:n.Kids+n.KidCount]
	next := n.Kids
	return f.b.mapped[symbol] && !t.Missing() && f.kids(at, id, &next, 0, pos)
}

func (f *flatBuilder) kids(parent, id uint32, next *uint32, inherited FieldID, running uint32) bool {
	structural := 0
	production := f.r.Trees[id].Production()
	for _, c := range f.r.Children(id) {
		t := &f.r.Trees[c]
		symbol := f.b.childSymbol(f.r, id, structural, t)
		field := FieldID(0)
		if !t.Extra {
			field = f.b.directField(production, structural)
			structural++
			if field == 0 {
				field = inherited
			}
		}
		if f.b.meta[symbol].Visible {
			at := *next
			*next++
			f.nodes[at] = FlatNode{Parent: parent, Field: f.b.fields[field]}
			if !f.node(at, c, symbol, running) {
				return false
			}
		} else if !f.kids(parent, c, next, field, running) {
			return false
		}
		running += t.Total()
	}
	return true
}
