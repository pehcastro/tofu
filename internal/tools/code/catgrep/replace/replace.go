package replace

import (
	"fmt"
	"strings"

	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/tree"
)

const (
	metaVarChar = '$'
	ellipsisLen = 3
)

func First(source string, l lang.Lang, p pattern.Pattern, rewrite string) (string, bool, error) {
	root, err := tree.Parse(source, l)
	if err != nil {
		return "", false, err
	}
	found, ok := p.Find(root.Node())
	if !ok {
		return source, false, nil
	}
	text, err := Text(rewrite, found.Env)
	if err != nil {
		return "", false, err
	}
	start := found.Node.Start()
	end, ok := p.MatchEnd(found.Node)
	if !ok {
		end = found.Node.End()
	}
	return source[:start] + reindent(text, 0, indentAt(source[:start])) + source[end:], true, nil
}

func Text(rewrite string, env *metavar.Env) (string, error) {
	var out strings.Builder
	last := 0
	for i := 0; i < len(rewrite); i++ {
		if rewrite[i] != metaVarChar {
			continue
		}
		name, multi, width, ok := splitVar(rewrite[i:])
		if !ok {
			continue
		}
		text, indent, err := lookup(env, name, multi, rewrite[i:i+width])
		if err != nil {
			return "", err
		}
		out.WriteString(rewrite[last:i])
		out.WriteString(reindent(text, indent, indentAt(rewrite[:i])))
		last = i + width
		i = last - 1
	}
	out.WriteString(rewrite[last:])
	return out.String(), nil
}

func splitVar(s string) (name string, multi bool, width int, ok bool) {
	dollars := 1
	for dollars < ellipsisLen && dollars < len(s) && s[dollars] == metaVarChar {
		dollars++
	}
	end := dollars
	for end < len(s) && (nameStart(s[end]) || s[end] >= '0' && s[end] <= '9') {
		end++
	}
	if end == dollars || !nameStart(s[dollars]) {
		return "", false, 0, false
	}
	return s[dollars:end], dollars == ellipsisLen, end, true
}

func nameStart(c byte) bool { return c >= 'A' && c <= 'Z' || c == '_' }

func Tree(rewrite string, l lang.Lang, env *metavar.Env) (string, error) {
	source := l.PreprocessPattern(rewrite)
	root, err := tree.Parse(source, l)
	if err != nil {
		return "", err
	}
	sigil := []rune(l.PreprocessPattern("$A"))[0]
	var out strings.Builder
	at := uint32(0)
	var splice func(n tree.Node) error
	splice = func(n tree.Node) error {
		if n.IsNamedLeaf() {
			if v, ok := metavar.Extract(n.Text(), sigil); ok {
				text, _, err := lookup(env, v.Name, v.IsEllipsis(), strings.ReplaceAll(n.Text(), string(sigil), string(metaVarChar)))
				if err != nil {
					return err
				}
				out.WriteString(source[at:n.Start()])
				out.WriteString(text)
				at = n.End()
				return nil
			}
		}
		for _, c := range n.Children() {
			if err := splice(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := splice(root.Node()); err != nil {
		return "", err
	}
	out.WriteString(source[at:root.Node().End()])
	return out.String(), nil
}

func lookup(env *metavar.Env, name string, multi bool, spelled string) (string, int, error) {
	nodes, ok := env.Multi(name)
	if !multi {
		node, found := env.Single(name)
		nodes, ok = []tree.Node{node}, found
	}
	if !ok {
		return "", 0, fmt.Errorf("rewrite names %s, which the pattern does not capture", spelled)
	}
	if len(nodes) == 0 {
		return "", 0, nil
	}
	source, start := nodes[0].Source(), nodes[0].Start()
	text := string(source[start:nodes[len(nodes)-1].End()])
	if !strings.Contains(text, "\n") {
		return text, 0, nil
	}
	return text, indentAt(source[:start]), nil
}
