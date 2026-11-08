package lang

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/konst"
)

type Fault struct {
	At      gotreesitter.Point
	Missing bool
	Kind    string
}

type Parser struct {
	Lang  Lang
	inner *gotreesitter.Parser
}

func NewParser(l Lang) Parser {
	inner := gotreesitter.NewParser(l.grammar())
	inner.SetTimeoutMicros(konst.CodemodRunMillis * 1000)
	return Parser{Lang: l, inner: inner}
}

func (p Parser) Parse(source []byte) (*gotreesitter.Tree, error) {
	tree, err := p.inner.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", p.Lang, err)
	}
	return tree, nil
}

func Parse(source []byte, l Lang) (*gotreesitter.Tree, error) { return NewParser(l).Parse(source) }

func (p Parser) BrokenRow(code []byte) (int, bool) {
	parsed, err := p.Parse(code)
	if err != nil {
		return 0, true
	}
	defer parsed.Release()
	if fault, broken := FirstFault(parsed); broken {
		return int(fault.At.Row), true
	}
	grammar := parsed.Language()
	pending := []*gotreesitter.Node{parsed.RootNode()}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if kind := node.Type(grammar); kind == "defer_statement" || kind == "go_statement" {
			called := node.NamedChild(0)
			for called != nil && called.Type(grammar) == "parenthesized_expression" {
				called = called.NamedChild(0)
			}
			if called == nil || called.Type(grammar) != "call_expression" {
				return int(node.StartPoint().Row), true
			}
		}
		for i := node.ChildCount() - 1; i >= 0; i-- {
			pending = append(pending, node.Child(i))
		}
	}
	return 0, false
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

func Scripts(source []byte, host, inner Lang) ([]byte, bool, error) {
	tree, err := Parse(source, host)
	if err != nil {
		return nil, false, err
	}
	defer tree.Release()
	grammar := tree.Language()
	masked := make([]byte, len(source))
	for i, b := range source {
		masked[i] = ' '
		if b == '\n' || b == '\r' {
			masked[i] = b
		}
	}
	found := false
	pending := []*gotreesitter.Node{tree.RootNode()}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		var written Lang
		switch node.Type(grammar) {
		case "script_element":
			written = JavaScript
		case "style_element":
			written = Css
		default:
			for i := node.ChildCount() - 1; i >= 0; i-- {
				pending = append(pending, node.Child(i))
			}
			continue
		}
		if value, ok := langAttribute(node, grammar, source); ok {
			written, ok = Named(value)
			if !ok {
				continue
			}
		}
		for i := range node.ChildCount() {
			if body := node.Child(i); written == inner && body.Type(grammar) == "raw_text" {
				copy(masked[body.StartByte():body.EndByte()], source[body.StartByte():body.EndByte()])
				found = true
			}
		}
	}
	return masked, found, nil
}

func langAttribute(element *gotreesitter.Node, grammar *gotreesitter.Language, source []byte) (string, bool) {
	if element.ChildCount() == 0 {
		return "", false
	}
	tag := element.Child(0)
	for i := range tag.ChildCount() {
		attribute := tag.Child(i)
		if attribute.Type(grammar) != "attribute" || attribute.ChildCount() < 2 || attribute.Child(0).Text(source) != "lang" {
			continue
		}
		return strings.Trim(attribute.Child(attribute.ChildCount()-1).Text(source), `"'`), true
	}
	return "", false
}
