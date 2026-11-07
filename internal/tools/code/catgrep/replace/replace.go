package replace

import (
	"bytes"
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
	layout      = " \t\r\n"
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
	edit, err := MakeEdit(p, found, rewrite)
	if err != nil {
		return "", false, err
	}
	return source[:edit.Start] + edit.Text + source[edit.End:], true, nil
}

type Edit struct {
	Start, End uint32
	Text       string
}

func MakeEdit(p pattern.Pattern, found pattern.Match, rewrite string) (Edit, error) {
	text, err := Text(rewrite, found.Env)
	if err != nil {
		return Edit{}, err
	}
	start := found.Node.Start()
	end, ok := p.MatchEnd(found.Node)
	if !ok {
		end = found.Node.End()
	}
	return Edit{Start: start, End: end, Text: Reindent(text, 0, IndentAt(found.Node.Source()[:start]))}, nil
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
		text, indent, err := lookup(env, name, multi, rewrite[:i], rewrite[i:i+width], rewrite[i+width:])
		if err != nil {
			return "", err
		}
		out.WriteString(rewrite[last:i])
		out.WriteString(Reindent(text, indent, IndentAt(rewrite[:i])))
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
	sigil := l.Sigil()
	var out strings.Builder
	at := uint32(len(l.PreprocessPattern("")))
	var splice func(n tree.Node) error
	splice = func(n tree.Node) error {
		if n.IsNamedLeaf() {
			if v, ok := metavar.Extract(n.Text(), sigil); ok {
				text, _, err := lookup(env, v.Name, v.IsEllipsis(), source[:n.Start()], strings.ReplaceAll(n.Text(), string(sigil), string(metaVarChar)), source[n.End():])
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

func lookup(env *metavar.Env, name string, multi bool, before, spelled, after string) (string, int, error) {
	if text, ok := env.Transformed(name); ok && !multi {
		return text, 0, nil
	}
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
	source, start, end := nodes[0].Source(), nodes[0].Start(), nodes[len(nodes)-1].End()
	if kept := bytes.TrimRight(source[:start], layout); len(kept) > 0 && strings.HasSuffix(before, string(kept[len(kept)-1])) {
		start = uint32(len(kept))
	}
	if kept := bytes.TrimLeft(source[end:], layout); len(kept) > 0 && strings.HasPrefix(after, string(kept[0])) {
		end = uint32(len(source) - len(kept))
	}
	text := string(source[start:end])
	if !strings.Contains(text, "\n") {
		return text, 0, nil
	}
	return text, IndentAt(source[:start]), nil
}
