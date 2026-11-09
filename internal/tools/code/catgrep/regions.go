package catgrep

import (
	"bytes"
	"go/scanner"
	"go/token"
	"slices"
	"strings"

	"tofu/internal/tools/code/catgrep/lang"
)

type split uint8

const (
	unsplit split = iota
	topLevel
	classBodies
)

type cut struct {
	at    int
	inner bool
}

func identByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func digit(c byte) bool { return c >= '0' && c <= '9' }

func tokensKept(searched, source []byte, sites []Site, reserved []string) bool {
	gluesOn := func(b byte) bool { return identByte(b) || b >= 0x80 }
	for _, s := range sites {
		glued := s.Start > 0 && gluesOn(source[s.Start-1]) || s.End < len(source) && gluesOn(source[s.End])
		if !bytes.Equal(searched[s.UnitStart:s.UnitEnd], source[s.UnitStart:s.UnitEnd]) || s.Before != s.After && (glued || !sameTokens(s.Before, s.After, reserved)) {
			return false
		}
	}
	return true
}

type goToken struct {
	kind     token.Token
	text     string
	newlines int
}

func sameTokens(before, after string, reserved []string) bool {
	was, wasRead := goTokens(before)
	is, isRead := goTokens(after)
	if !wasRead || !isRead || len(was) != len(is) {
		return false
	}
	for i, old := range was {
		renamed := old.kind == token.IDENT && identifierShaped(old.text) && identifierShaped(is[i].text) && !slices.Contains(reserved, old.text) && !slices.Contains(reserved, is[i].text)
		if old.kind != is[i].kind || old.newlines != is[i].newlines || old.text != is[i].text && !renamed {
			return false
		}
	}
	return true
}

func goTokens(code string) ([]goToken, bool) {
	file := token.NewFileSet().AddFile("", -1, len(code))
	failed := false
	var s scanner.Scanner
	s.Init(file, []byte(code), func(token.Position, string) { failed = true }, scanner.ScanComments)
	var read []goToken
	for end := 0; ; {
		at, kind, text := s.Scan()
		if kind == token.SEMICOLON && text == "\n" {
			continue
		}
		if text == "" {
			text = kind.String()
		}
		offset := file.Offset(at)
		read = append(read, goToken{kind: kind, text: text, newlines: strings.Count(code[end:offset], "\n")})
		if kind == token.EOF || kind == token.ILLEGAL {
			return read, kind == token.EOF && !failed
		}
		end = offset + len(text)
	}
}

func maximalMunch(l lang.Lang) bool {
	switch l {
	case lang.Go, lang.Python, lang.Rust, lang.TypeScript, lang.Tsx, lang.JavaScript:
		return true
	}
	return false
}

func identifierShaped(word string) bool {
	if word == "" || digit(word[0]) {
		return false
	}
	for i := range len(word) {
		if !identByte(word[i]) {
			return false
		}
	}
	return true
}

func nextWord(source []byte, word string, from int, whole bool) int {
	for from <= len(source) {
		at := bytes.Index(source[from:], []byte(word))
		if at < 0 {
			return -1
		}
		at += from
		if !whole || standsAlone(source, at, len(word)) {
			return at
		}
		from = at + 1
	}
	return -1
}

func standsAlone(source []byte, at, size int) bool {
	start, end := at, at+size
	for start > 0 && identByte(source[start-1]) {
		start--
	}
	for end < len(source) && identByte(source[end]) {
		end++
	}
	return start == at && end == at+size || digit(source[start])
}

func splitFor(l lang.Lang, kind string) split {
	switch {
	case l == lang.Go:
		return topLevel
	case l != lang.Python:
		return unsplit
	case kind == "module" || kind == "block" || kind == "class_definition" || kind == "decorated_definition":
		return topLevel
	}
	return classBodies
}

func (s *scratch) mask(l lang.Lang, source []byte, need literals) ([]byte, bool) {
	cuts, ok := regionCuts(l, source, s.cuts, need.split)
	s.cuts = cuts
	if !ok || len(cuts) == 1 {
		return nil, true
	}
	s.hits = slices.Grow(s.hits[:0], len(cuts))[:len(cuts)]
	clear(s.hits)
	for k, word := range need.words {
		whole, region := identifierShaped(word), 0
		for at := nextWord(source, word, 0, whole); at >= 0; at = nextWord(source, word, at+1, whole) {
			for region+1 < len(cuts) && cuts[region+1].at <= at {
				region++
			}
			if s.hits[region] == k {
				s.hits[region] = k + 1
			}
		}
	}
	s.keep = slices.Grow(s.keep[:0], len(cuts))[:len(cuts)]
	for top := 0; top < len(cuts); {
		end := top + 1
		for end < len(cuts) && cuts[end].inner {
			end++
		}
		held := false
		for r := top; r < end; r++ {
			s.keep[r] = s.hits[r] == len(need.words)
			held = held || s.keep[r]
		}
		s.keep[top], s.keep[end-1] = s.keep[top] || held, s.keep[end-1] || held
		top = end
	}
	kept := 0
	for _, keep := range s.keep {
		if keep {
			kept++
		}
	}
	switch kept {
	case 0:
		return nil, false
	case len(cuts):
		return nil, true
	}
	s.masked = append(s.masked[:0], source...)
	for r, c := range cuts {
		if s.keep[r] {
			continue
		}
		end := len(source)
		if r+1 < len(cuts) {
			end = cuts[r+1].at
		}
		for i := c.at; i < end; i++ {
			if b := s.masked[i]; b != '\n' && b != '\r' {
				s.masked[i] = ' '
			}
		}
	}
	return s.masked, true
}

func regionCuts(l lang.Lang, source []byte, into []cut, depth split) ([]cut, bool) {
	into = append(into[:0], cut{})
	if bytes.Count(source, []byte("\r")) != bytes.Count(source, []byte("\r\n")) {
		return into, false
	}
	if l == lang.Go {
		return goCuts(source, into)
	}
	lines := pythonLines{cuts: into, classes: depth == classBodies, decorated: bytes.HasPrefix(source, []byte("@"))}
	lines.class = lines.classes && startsClass(source)
	return lines.scan(source)
}

func startsClass(line []byte) bool {
	return bytes.HasPrefix(line, []byte("class ")) || bytes.HasPrefix(line, []byte("class\t"))
}

func lineEnd(source []byte, from int) int {
	if at := bytes.IndexByte(source[from:], '\n'); at >= 0 {
		return from + at
	}
	return len(source)
}

func quotedEnd(source []byte, from int, quote byte) int {
	for i := from + 1; i < len(source); i++ {
		switch source[i] {
		case '\\':
			i++
		case '\n':
			return -1
		case quote:
			return i
		}
	}
	return -1
}

func goCuts(source []byte, into []cut) ([]cut, bool) {
	depth, last := 0, 0
	for i := 0; i < len(source); i++ {
		switch c := source[i]; c {
		case '/':
			switch {
			case i+1 < len(source) && source[i+1] == '/':
				i = lineEnd(source, i) - 1
				continue
			case i+1 < len(source) && source[i+1] == '*':
				end := bytes.Index(source[i+2:], []byte("*/"))
				if end < 0 {
					return into, false
				}
				i += end + 3
				continue
			}
		case '"', '\'':
			if i = quotedEnd(source, i, c); i < 0 {
				return into, false
			}
		case '`':
			end := bytes.IndexByte(source[i+1:], '`')
			if end < 0 {
				return into, false
			}
			i += end + 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth--; depth < 0 {
				return into, false
			}
		case '\n':
			if depth == 0 && goDeclares(source[i+1:]) && goTerminated(source[:last]) {
				into = append(into, cut{at: i + 1})
			}
			continue
		case ' ', '\t', '\r':
			continue
		}
		last = i + 1
	}
	return into, true
}

func goDeclares(line []byte) bool {
	for _, keyword := range [...]string{"func", "type", "var", "const", "import"} {
		if after, ok := bytes.CutPrefix(line, []byte(keyword)); ok && len(after) > 0 && (after[0] == ' ' || after[0] == '\t' || after[0] == '(') {
			return true
		}
	}
	return false
}

func goTerminated(code []byte) bool {
	if len(code) == 0 {
		return true
	}
	switch c := code[len(code)-1]; {
	case c == ')' || c == ']' || c == '}' || c == '"' || c == '\'' || c == '`':
		return true
	case !identByte(c):
		return false
	}
	start := len(code)
	for start > 0 && identByte(code[start-1]) {
		start--
	}
	if digit(code[start]) {
		return true
	}
	switch string(code[start:]) {
	case "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func", "go", "goto",
		"if", "import", "interface", "map", "package", "range", "return", "select", "struct", "switch", "type", "var":
		return false
	}
	return true
}

type pythonLines struct {
	cuts      []cut
	top       int
	classes   bool
	decorated bool
	class     bool
	indent    int
	inner     bool
}

func (p *pythonLines) scan(source []byte) ([]cut, bool) {
	depth, last := 0, -1
	for i := 0; i < len(source); i++ {
		switch c := source[i]; c {
		case '#':
			i = lineEnd(source, i) - 1
			continue
		case '"', '\'':
			if i = pythonStringEnd(source, i); i < 0 {
				return p.cuts, false
			}
		case '\\':
			switch {
			case i+1 < len(source) && source[i+1] == '\n':
				i++
			case i+2 < len(source) && source[i+1] == '\r' && source[i+2] == '\n':
				i += 2
			default:
				return p.cuts, false
			}
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth--; depth < 0 {
				return p.cuts, false
			}
		case '\n':
			if depth == 0 && i+1 < len(source) {
				p.line(source[i+1:], i+1, pythonTerminated(source, last))
			}
			continue
		case ' ', '\t', '\r', '\f':
			continue
		}
		last = i
	}
	return p.cuts, true
}

func (p *pythonLines) line(line []byte, at int, terminated bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent == len(line) {
		return
	}
	if line[indent] == '\t' || line[indent] == '\f' {
		if p.class {
			p.abandon()
		}
		return
	}
	code, opens := pythonLineStart(line[indent:])
	switch {
	case !code:
	case indent == 0:
		boundary := opens && !p.decorated && terminated
		if p.class && !boundary {
			p.abandon()
		}
		if boundary {
			p.cuts = append(p.cuts, cut{at: at})
			p.top = len(p.cuts) - 1
		}
		p.decorated = line[0] == '@'
		p.class = p.classes && startsClass(line)
		p.indent, p.inner = 0, false
	case !p.class:
	case p.indent == 0 || indent == p.indent:
		first := p.indent == 0
		p.indent = indent
		if opens && !p.inner && (first || terminated) {
			p.cuts = append(p.cuts, cut{at: at, inner: true})
		}
		p.inner = line[indent] == '@'
	case indent < p.indent:
		p.abandon()
	}
}

func (p *pythonLines) abandon() {
	p.cuts = p.cuts[:p.top+1]
	p.class = false
}

func pythonTerminated(source []byte, last int) bool {
	if last < 0 {
		return true
	}
	switch c := source[last]; c {
	case ')', ']', '}', '"', '\'', '*', '.', ';', ',':
		return true
	default:
		return identByte(c)
	}
}

func pythonLineStart(line []byte) (code, opens bool) {
	switch c := line[0]; {
	case c == '\r' || c == '\n' || c == '#':
		return false, false
	case c == '"' || c == '\'' || c >= 0x80 || c == ')' || c == ']' || c == '}' || c == '\\':
		return true, false
	}
	word := 0
	for word < len(line) && identByte(line[word]) {
		word++
	}
	if word < len(line) && (line[word] == '"' || line[word] == '\'' || line[word] >= 0x80) {
		return true, false
	}
	switch string(line[:word]) {
	case "else", "elif", "except", "finally":
		return true, false
	}
	return true, true
}

func pythonStringEnd(source []byte, at int) int {
	quote, formatted := source[at], false
	prefix := at
	for prefix > 0 && at-prefix < 2 && bytes.IndexByte([]byte("rRbBuUfFtT"), source[prefix-1]) >= 0 {
		prefix--
	}
	if prefix == 0 || !identByte(source[prefix-1]) && source[prefix-1] < 0x80 {
		formatted = bytes.ContainsAny(source[prefix:at], "fFtT")
	}
	triple := at+2 < len(source) && source[at+1] == quote && source[at+2] == quote
	i := at + 1
	if triple {
		i = at + 3
	}
	for ; i < len(source); i++ {
		switch c := source[i]; {
		case c == '\\':
			if i+1 < len(source) && (source[i+1] == '\n' && !triple || formatted && (source[i+1] == '{' || source[i+1] == '}')) {
				return -1
			}
			i++
		case c == '\n' && !triple:
			return -1
		case c == quote && !triple:
			return i
		case c == quote && i+2 < len(source) && source[i+1] == quote && source[i+2] == quote:
			return i + 2
		case c == '{' && formatted && i+1 < len(source) && source[i+1] == '{':
			i++
		case c == '{' && formatted:
			if i = replacementEnd(source, i, triple); i < 0 {
				return -1
			}
		}
	}
	return -1
}

func replacementEnd(source []byte, at int, triple bool) int {
	depth := 0
	for i := at; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		case '"', '\'', '#', '\\':
			return -1
		case '\n':
			if !triple {
				return -1
			}
		}
	}
	return -1
}
