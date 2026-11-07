package lang

import (
	"fmt"

	"github.com/odvcencio/gotreesitter"
)

type Fault struct {
	At      gotreesitter.Point
	Missing bool
	Kind    string
}

func Parse(source []byte, l Lang) (*gotreesitter.Tree, error) {
	tree, err := gotreesitter.NewParser(l.grammar()).Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", l, err)
	}
	return tree, nil
}

func FirstFault(tree *gotreesitter.Tree) (Fault, bool) {
	pending := []*gotreesitter.Node{tree.RootNode()}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if node.IsError() || node.IsMissing() {
			return Fault{At: node.StartPoint(), Missing: node.IsMissing(), Kind: node.Type(tree.Language())}, true
		}
		for i := node.ChildCount() - 1; i >= 0; i-- {
			pending = append(pending, node.Child(i))
		}
	}
	return Fault{}, false
}
