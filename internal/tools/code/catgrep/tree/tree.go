package tree

import (
	"fmt"
	"iter"
	"strings"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/konst"
	"tofu/internal/tools/code/catgrep/lang"
)

type Parser struct {
	Lang  lang.Lang
	inner *gotreesitter.Parser
	work  gotreesitter.FlatWork
	spare []gotreesitter.FlatNode
}

type Root struct {
	flat   []gotreesitter.FlatNode
	old    *gotreesitter.Tree
	lang   *gotreesitter.Language
	source []byte
	owner  *Parser
}

type Node struct {
	root *Root
	i    uint32
	old  *gotreesitter.Node
}

func NewParser(l lang.Lang) *Parser {
	inner := gotreesitter.NewParser(l.Grammar())
	inner.SetTimeoutMicros(konst.CodemodRunMillis * 1000)
	return &Parser{Lang: l, inner: inner}
}

func Parse(source string, l lang.Lang) (*Root, error) {
	bytes := []byte(source)
	parsed, err := lang.Parse(bytes, l)
	if err != nil {
		return nil, err
	}
	return &Root{old: parsed, lang: parsed.Language(), source: bytes}, nil
}

func (p *Parser) Parse(source []byte) (*Root, error) {
	flat, old, err := p.inner.ParseFlatWith(&p.work, source, p.spare)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", p.Lang, err)
	}
	if old != nil {
		p.spare = flat
		return &Root{old: old, lang: old.Language(), source: source}, nil
	}
	p.spare = nil
	return &Root{flat: flat, lang: p.inner.Language(), source: source, owner: p}, nil
}

func (r *Root) Clean() bool { return r.old == nil }

func (r *Root) Faulted(checks lang.Checks) bool {
	return r.old != nil || checks.Faulted(r.flat, r.source)
}

func (r *Root) Release() {
	switch {
	case r.old != nil:
		r.old.Release()
	case r.owner != nil:
		r.owner.spare, r.owner = r.flat, nil
	}
}

func (r *Root) Node() Node {
	if r.old != nil {
		return Node{root: r, old: r.old.RootNode()}
	}
	return Node{root: r}
}

func (n Node) at() *gotreesitter.FlatNode { return &n.root.flat[n.i] }

func (n Node) Kind() string {
	if n.root.old != nil {
		return n.old.Type(n.root.lang)
	}
	return n.at().Type(n.root.lang)
}

func (n Node) KindID() gotreesitter.Symbol {
	if n.root.old != nil {
		return n.root.lang.PublicSymbolForNamedness(n.old.Symbol(), n.old.IsNamed())
	}
	return n.root.lang.PublicSymbolForNamedness(n.at().Symbol, n.IsNamed())
}

func (n Node) Same(other Node) bool { return n == other }

func (n Node) flag(f gotreesitter.FlatFlags) bool { return n.at().Flags&f != 0 }

func (n Node) IsNamed() bool {
	if n.root.old != nil {
		return n.old.IsNamed()
	}
	return n.flag(gotreesitter.FlatNamed)
}

func (n Node) IsExtra() bool {
	if n.root.old != nil {
		return n.old.IsExtra()
	}
	return n.flag(gotreesitter.FlatExtra)
}

func (n Node) IsMissing() bool { return n.root.old != nil && n.old.IsMissing() }

func (n Node) IsError() bool {
	if n.root.old != nil {
		return n.old.IsError()
	}
	return n.at().IsError()
}

func (n Node) IsLeaf() bool { return n.ChildCount() == 0 }

func (n Node) IsNamedLeaf() bool {
	if n.root.old != nil {
		return n.old.NamedChildCount() == 0
	}
	for i := range n.ChildCount() {
		if n.Child(i).IsNamed() {
			return false
		}
	}
	return true
}

func (n Node) Start() uint32 {
	if n.root.old != nil {
		return n.old.StartByte()
	}
	return n.at().Start
}

func (n Node) End() uint32 {
	if n.root.old != nil {
		return n.old.EndByte()
	}
	return n.at().End
}

func (n Node) Source() []byte { return n.root.source }

func (n Node) Text() string { return string(n.Bytes()) }

func (n Node) Bytes() []byte { return n.root.source[n.Start():n.End()] }

func (n Node) Parent() (Node, bool) {
	if n.root.old != nil {
		parent := n.old.Parent()
		return Node{root: n.root, old: parent}, parent != nil
	}
	return Node{root: n.root, i: n.at().Parent}, n.i != 0
}

func (n Node) Field(name string) (Node, bool) {
	if n.root.old != nil {
		child := n.old.ChildByFieldName(name, n.root.lang)
		return Node{root: n.root, old: child}, child != nil
	}
	id, ok := n.root.lang.FieldByName(name)
	for i := range n.ChildCount() {
		if child := n.Child(i); ok && id != 0 && child.at().Field == id {
			return child, true
		}
	}
	return Node{}, false
}

func (n Node) ChildCount() int {
	if n.root.old != nil {
		return n.old.ChildCount()
	}
	return int(n.at().KidCount)
}

func (n Node) Child(i int) Node {
	if n.root.old != nil {
		return Node{root: n.root, old: n.old.Child(i)}
	}
	return Node{root: n.root, i: n.at().Kids + uint32(i)}
}

func (n Node) Children() []Node {
	children := make([]Node, n.ChildCount())
	for i := range children {
		children[i] = n.Child(i)
	}
	return children
}

func (n Node) PreOrder() iter.Seq[Node] {
	return func(yield func(Node) bool) {
		pending := []Node{n}
		for len(pending) > 0 {
			next := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if !yield(next) {
				return
			}
			for i := next.ChildCount() - 1; i >= 0; i-- {
				pending = append(pending, next.Child(i))
			}
		}
	}
}

func (n Node) SExpr() string {
	if n.root.old != nil {
		return n.old.SExpr(n.root.lang)
	}
	if !n.IsNamed() && !n.IsError() {
		return ""
	}
	var b strings.Builder
	n.writeSExpr(&b)
	return b.String()
}

func (n Node) writeSExpr(b *strings.Builder) {
	b.WriteString("(" + n.Kind())
	for i := range n.ChildCount() {
		if child := n.Child(i); child.IsNamed() || child.IsError() {
			b.WriteByte(' ')
			child.writeSExpr(b)
		}
	}
	b.WriteByte(')')
}
