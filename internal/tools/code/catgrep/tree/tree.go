package tree

import (
	"iter"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/tools/code/catgrep/lang"
)

type Root struct {
	tree   *gotreesitter.Tree
	source []byte
}

type Node struct {
	inner *gotreesitter.Node
	root  *Root
}

func Parse(source string, l lang.Lang) (*Root, error) {
	return ParseWith(lang.NewParser(l), []byte(source))
}

func ParseWith(parser lang.Parser, source []byte) (*Root, error) {
	parsed, err := parser.Parse(source)
	if err != nil {
		return nil, err
	}
	return &Root{tree: parsed, source: source}, nil
}

func (r *Root) Release() { r.tree.Release() }

func (r *Root) Node() Node { return Node{inner: r.tree.RootNode(), root: r} }

func (n Node) Kind() string { return n.inner.Type(n.root.tree.Language()) }

func (n Node) KindID() gotreesitter.Symbol {
	return n.root.tree.Language().PublicSymbolForNamedness(n.inner.Symbol(), n.inner.IsNamed())
}

func (n Node) Same(other Node) bool { return n.inner == other.inner }

func (n Node) IsNamed() bool { return n.inner.IsNamed() }

func (n Node) IsExtra() bool { return n.inner.IsExtra() }

func (n Node) IsMissing() bool { return n.inner.IsMissing() }

func (n Node) IsError() bool { return n.inner.IsError() }

func (n Node) IsLeaf() bool { return n.inner.ChildCount() == 0 }

func (n Node) IsNamedLeaf() bool { return n.inner.NamedChildCount() == 0 }

func (n Node) Start() uint32 { return n.inner.StartByte() }

func (n Node) End() uint32 { return n.inner.EndByte() }

func (n Node) Source() []byte { return n.root.source }

func (n Node) Text() string { return n.inner.Text(n.root.source) }

func (n Node) Bytes() []byte { return n.root.source[n.inner.StartByte():n.inner.EndByte()] }

func (n Node) Parent() (Node, bool) {
	parent := n.inner.Parent()
	return Node{inner: parent, root: n.root}, parent != nil
}

func (n Node) Field(name string) (Node, bool) {
	child := n.inner.ChildByFieldName(name, n.root.tree.Language())
	return Node{inner: child, root: n.root}, child != nil
}

func (n Node) ChildCount() int { return n.inner.ChildCount() }

func (n Node) Child(i int) Node { return Node{inner: n.inner.Child(i), root: n.root} }

func (n Node) Children() []Node {
	inner := n.inner.Children()
	children := make([]Node, len(inner))
	for i, c := range inner {
		children[i] = Node{inner: c, root: n.root}
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

func (n Node) SExpr() string { return n.inner.SExpr(n.root.tree.Language()) }
