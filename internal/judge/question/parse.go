package question

import (
	"fmt"
	"strconv"
	"strings"
)

type nodeKind int

const (
	nodeScalar nodeKind = iota
	nodeMap
	nodeList
)

type node struct {
	kind   nodeKind
	text   string
	keys   []string
	fields map[string]*node
	items  []*node
	file   string
	line   int
}

func (n *node) child(key string) (*node, bool) {
	if n == nil || n.kind != nodeMap {
		return nil, false
	}
	c, ok := n.fields[key]
	return c, ok
}

func (n *node) set(key string, value *node) {
	if _, ok := n.fields[key]; !ok {
		n.keys = append(n.keys, key)
	}
	n.fields[key] = value
}

type source struct {
	file  string
	lines []string
	nums  []int
}

func (s *source) blank(i int) bool {
	t := strings.TrimSpace(s.lines[i])
	return t == "" || strings.HasPrefix(t, "#")
}

func (s *source) indent(i int) int {
	l := s.lines[i]
	return len(l) - len(strings.TrimLeft(l, " "))
}

func (s *source) content(i int) string {
	return strings.TrimSpace(s.lines[i])
}

func (s *source) next(i int) int {
	for i < len(s.lines) && s.blank(i) {
		i++
	}
	return i
}

func parse(file string, data []byte) (*node, error) {
	s := &source{file: file}
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.ContainsRune(leading(raw), '\t') {
			return nil, fmt.Errorf("%s:%d: a tab in the indent", file, i+1)
		}
		s.lines = append(s.lines, strings.TrimRight(raw, " \r"))
		s.nums = append(s.nums, i+1)
	}
	i := s.next(0)
	if i >= len(s.lines) {
		return nil, fmt.Errorf("%s: the file has no content", file)
	}
	root, i, err := s.parseBlock(i, s.indent(i))
	if err != nil {
		return nil, err
	}
	i = s.next(i)
	if i < len(s.lines) {
		return nil, fmt.Errorf("%s:%d: content after the end of the document", file, s.nums[i])
	}
	return root, nil
}

func (s *source) parseBlock(i, indent int) (*node, int, error) {
	i = s.next(i)
	if i >= len(s.lines) {
		return nil, i, fmt.Errorf("%s: the document ends where a value was expected", s.file)
	}
	c := s.content(i)
	if c == "-" || strings.HasPrefix(c, "- ") {
		return s.parseList(i, indent)
	}
	return s.parseMap(i, indent)
}

func (s *source) parseMap(i, indent int) (*node, int, error) {
	m := &node{kind: nodeMap, fields: map[string]*node{}, file: s.file, line: s.nums[i]}
	j, err := s.parseMapInto(m, i, indent)
	if err != nil {
		return nil, j, err
	}
	return m, j, nil
}

func (s *source) parseMapInto(m *node, i, indent int) (int, error) {
	for {
		i = s.next(i)
		if i >= len(s.lines) || s.indent(i) < indent {
			return i, nil
		}
		if s.indent(i) > indent {
			return i, fmt.Errorf("%s:%d: an indent where a key was expected", s.file, s.nums[i])
		}
		key, rest, ok := splitKey(s.content(i))
		if !ok {
			return i, fmt.Errorf("%s:%d: expected a key, found %q", s.file, s.nums[i], s.content(i))
		}
		if _, dup := m.fields[key]; dup {
			return i, fmt.Errorf("%s:%d: the key %q appears twice", s.file, s.nums[i], key)
		}
		line := s.nums[i]
		value, j, err := s.parseValue(i+1, indent, rest, line)
		if err != nil {
			return j, err
		}
		m.set(key, value)
		i = j
	}
}

func (s *source) parseValue(i, indent int, rest string, line int) (*node, int, error) {
	if head, ok := blockHeader(rest); ok {
		text, j, err := s.parseBlockScalar(i, indent, head)
		if err != nil {
			return nil, j, err
		}
		return &node{kind: nodeScalar, text: text, file: s.file, line: line}, j, nil
	}
	if rest != "" {
		text, err := scalar(rest)
		if err != nil {
			return nil, i, fmt.Errorf("%s:%d: %w", s.file, line, err)
		}
		return &node{kind: nodeScalar, text: text, file: s.file, line: line}, i, nil
	}
	j := s.next(i)
	if j < len(s.lines) && s.indent(j) == indent {
		c := s.content(j)
		if c == "-" || strings.HasPrefix(c, "- ") {
			return s.parseList(j, indent)
		}
	}
	if j >= len(s.lines) || s.indent(j) <= indent {
		return &node{kind: nodeScalar, file: s.file, line: line}, i, nil
	}
	return s.parseBlock(j, s.indent(j))
}

func (s *source) parseList(i, indent int) (*node, int, error) {
	l := &node{kind: nodeList, file: s.file, line: s.nums[i]}
	for {
		i = s.next(i)
		if i >= len(s.lines) || s.indent(i) < indent {
			return l, i, nil
		}
		if s.indent(i) > indent {
			return nil, i, fmt.Errorf("%s:%d: an indent where a list item was expected", s.file, s.nums[i])
		}
		c := s.content(i)
		if c != "-" && !strings.HasPrefix(c, "- ") {
			return l, i, nil
		}
		rest := strings.TrimSpace(strings.TrimPrefix(c, "-"))
		item, j, err := s.parseItem(i, indent, rest, s.nums[i])
		if err != nil {
			return nil, j, err
		}
		l.items = append(l.items, item)
		i = j
	}
}

func (s *source) parseItem(i, indent int, rest string, line int) (*node, int, error) {
	if rest == "" {
		j := s.next(i + 1)
		if j >= len(s.lines) || s.indent(j) <= indent {
			return &node{kind: nodeScalar, file: s.file, line: line}, i + 1, nil
		}
		return s.parseBlock(j, s.indent(j))
	}
	if head, ok := blockHeader(rest); ok {
		text, j, err := s.parseBlockScalar(i+1, indent, head)
		if err != nil {
			return nil, j, err
		}
		return &node{kind: nodeScalar, text: text, file: s.file, line: line}, j, nil
	}
	if key, kr, ok := splitKey(rest); ok {
		m := &node{kind: nodeMap, fields: map[string]*node{}, file: s.file, line: line}
		value, j, err := s.parseValue(i+1, indent+2, kr, line)
		if err != nil {
			return nil, j, err
		}
		m.set(key, value)
		j, err = s.parseMapInto(m, j, indent+2)
		if err != nil {
			return nil, j, err
		}
		return m, j, nil
	}
	text, err := scalar(rest)
	if err != nil {
		return nil, i, fmt.Errorf("%s:%d: %w", s.file, line, err)
	}
	return &node{kind: nodeScalar, text: text, file: s.file, line: line}, i + 1, nil
}

type blockHead struct {
	fold bool
	keep bool
}

func blockHeader(rest string) (blockHead, bool) {
	switch rest {
	case "|":
		return blockHead{fold: false, keep: true}, true
	case "|-":
		return blockHead{fold: false, keep: false}, true
	case ">":
		return blockHead{fold: true, keep: true}, true
	case ">-":
		return blockHead{fold: true, keep: false}, true
	}
	return blockHead{}, false
}

func (s *source) parseBlockScalar(i, indent int, head blockHead) (string, int, error) {
	var raw []string
	body := -1
	last := i
	for i < len(s.lines) {
		if strings.TrimSpace(s.lines[i]) == "" {
			raw = append(raw, "")
			i++
			continue
		}
		if s.indent(i) <= indent {
			break
		}
		if body < 0 {
			body = s.indent(i)
		}
		if s.indent(i) < body {
			return "", i, fmt.Errorf("%s:%d: the block scalar is under indented", s.file, s.nums[i])
		}
		raw = append(raw, s.lines[i][body:])
		last = i
		i++
	}
	if body < 0 {
		return "", i, fmt.Errorf("%s:%d: the block scalar has no content", s.file, s.nums[last])
	}
	for len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	var b strings.Builder
	for k, l := range raw {
		if !head.fold {
			if k > 0 {
				b.WriteString("\n")
			}
			b.WriteString(l)
			continue
		}
		if l == "" {
			b.WriteString("\n")
			continue
		}
		if k > 0 && raw[k-1] != "" {
			b.WriteString(" ")
		}
		b.WriteString(l)
	}
	text := b.String()
	if head.keep {
		text += "\n"
	}
	return text, i, nil
}

func scalar(rest string) (string, error) {
	if strings.HasPrefix(rest, "\"") {
		if len(rest) < 2 || !strings.HasSuffix(rest, "\"") {
			return "", fmt.Errorf("the double quoted value does not close")
		}
		return unquote(rest[1 : len(rest)-1])
	}
	if strings.HasPrefix(rest, "'") {
		if len(rest) < 2 || !strings.HasSuffix(rest, "'") {
			return "", fmt.Errorf("the single quoted value does not close")
		}
		return strings.ReplaceAll(rest[1:len(rest)-1], "''", "'"), nil
	}
	return rest, nil
}

func unquote(in string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(in); i++ {
		c := in[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(in) {
			return "", fmt.Errorf("the value ends in a backslash")
		}
		switch in[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'u':
			if i+4 >= len(in) {
				return "", fmt.Errorf("a short unicode escape")
			}
			v, err := strconv.ParseUint(in[i+1:i+5], 16, 32)
			if err != nil {
				return "", fmt.Errorf("a bad unicode escape %q", in[i+1:i+5])
			}
			b.WriteRune(rune(v))
			i += 4
		default:
			return "", fmt.Errorf("an unknown escape \\%c", in[i])
		}
	}
	return b.String(), nil
}

func splitKey(in string) (string, string, bool) {
	i := strings.IndexByte(in, ':')
	if i < 0 {
		return "", "", false
	}
	key := in[:i]
	if key == "" || strings.ContainsAny(key, " \"'#") {
		return "", "", false
	}
	rest := in[i+1:]
	if rest == "" {
		return key, "", true
	}
	if rest[0] != ' ' {
		return "", "", false
	}
	return key, strings.TrimSpace(rest), true
}

func leading(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
