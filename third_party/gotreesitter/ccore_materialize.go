package gotreesitter

import "github.com/odvcencio/gotreesitter/internal/ccore"

type ccoreBuilder struct {
	b        *ccoreBinding
	r        *ccore.Result
	arena    *nodeArena
	nodes    []Node
	kids     []*Node
	fields   []FieldID
	metas    []nodeFieldMetadata
	unmapped bool
}

func (b *ccoreBinding) materialize(r *ccore.Result, source []byte) (*Tree, bool) {
	total := 1 + b.countVisible(r, r.Root)
	m := &ccoreBuilder{b: b, r: r, nodes: make([]Node, total), kids: make([]*Node, 0, total), fields: make([]FieldID, total)}
	m.arena = &nodeArena{nodes: m.nodes}
	m.nodes = m.nodes[:1]
	root := &m.nodes[0]
	m.build(root, r.Root, r.Trees[r.Root].Symbol, ccore.Length{})
	if m.unmapped {
		return nil, false
	}
	tree := NewTree(root, source, b.target)
	tree.incrementalReuseDisabled = true
	tree.incrementalReuseUnsupportedClause = newCoreIncrementalReuseClause
	return tree, true
}

func (b *ccoreBinding) childSymbol(r *ccore.Result, parent uint32, structural int, child *ccore.Subtree) uint16 {
	if child.Extra {
		return child.Symbol
	}
	if row := b.source.AliasSequences[r.Trees[parent].ProductionID]; structural < len(row) && row[structural] != 0 {
		return uint16(row[structural])
	}
	return child.Symbol
}

func (b *ccoreBinding) countVisible(r *ccore.Result, id uint32) int {
	n, structural := 0, 0
	for _, c := range r.Children(id) {
		t := &r.Trees[c]
		if b.source.SymbolMetadata[b.childSymbol(r, id, structural, t)].Visible {
			n++
		}
		if !t.Extra {
			structural++
		}
		n += b.countVisible(r, c)
	}
	return n
}

func (b *ccoreBinding) visibleChildren(r *ccore.Result, id uint32) int {
	n, structural := 0, 0
	for _, c := range r.Children(id) {
		t := &r.Trees[c]
		if b.source.SymbolMetadata[b.childSymbol(r, id, structural, t)].Visible {
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

func (m *ccoreBuilder) build(n *Node, id uint32, symbol uint16, pos ccore.Length) {
	t := &m.r.Trees[id]
	start := pos.Add(t.Padding)
	end := start.Add(t.Size)
	if !m.b.mapped[symbol] {
		m.unmapped = true
	}
	n.ownerArena = m.arena
	n.symbol = m.b.symbols[symbol]
	n.setNamed(m.b.source.SymbolMetadata[symbol].Named)
	n.setExtra(t.Extra)
	n.startByte, n.endByte = start.Bytes, end.Bytes
	n.startPoint, n.endPoint = Point{start.Row, start.Column}, Point{end.Row, end.Column}
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
			n.fieldMetadata = m.meta(fields)
			break
		}
	}
}

func (m *ccoreBuilder) fill(parent *Node, id uint32, kids []*Node, fields []FieldID, k *int, inherited FieldID, supertypes uint32, running ccore.Length) {
	structural := 0
	production := m.r.Trees[id].ProductionID
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
		if m.b.source.SymbolMetadata[symbol].Visible {
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
		running = running.Add(t.Total())
	}
}

func (m *ccoreBuilder) meta(fields []FieldID) *nodeFieldMetadata {
	if len(m.metas) == cap(m.metas) {
		m.metas = make([]nodeFieldMetadata, 0, 1024)
	}
	m.metas = append(m.metas, nodeFieldMetadata{ids: fields})
	return &m.metas[len(m.metas)-1]
}
