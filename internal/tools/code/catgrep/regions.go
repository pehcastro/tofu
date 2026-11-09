package catgrep

import (
	"bytes"
	"go/scanner"
	"go/token"
	"slices"
	"strings"
	"unicode/utf8"

	"tofu/internal/tools/code/catgrep/lang"
)

type split uint8

const (
	unsplit split = iota
	topLevel
	bodies
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
	was, is := newGoScan(before), newGoScan(after)
	for {
		old, now := was.next(), is.next()
		renamed := old.kind == token.IDENT && identifierShaped(old.text) && identifierShaped(now.text) && !slices.Contains(reserved, old.text) && !slices.Contains(reserved, now.text)
		if old.kind != now.kind || old.newlines != now.newlines || old.text != now.text && !renamed {
			return false
		}
		if old.kind == token.EOF || old.kind == token.ILLEGAL {
			return old.kind == token.EOF && !was.failed && !is.failed
		}
	}
}

type goScan struct {
	code    string
	file    *token.File
	scanner scanner.Scanner
	end     int
	failed  bool
}

func newGoScan(code string) *goScan {
	g := &goScan{code: code, file: token.NewFileSet().AddFile("", -1, len(code))}
	g.scanner.Init(g.file, []byte(code), func(token.Position, string) { g.failed = true }, scanner.ScanComments)
	return g
}

func (g *goScan) next() goToken {
	for {
		at, kind, text := g.scanner.Scan()
		if kind == token.SEMICOLON && text == "\n" {
			continue
		}
		if text == "" {
			text = kind.String()
		}
		offset := g.file.Offset(at)
		read := goToken{kind: kind, text: text, newlines: strings.Count(g.code[g.end:offset], "\n")}
		g.end = offset + len(text)
		return read
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
	switch l {
	case lang.Go:
		return topLevel
	case lang.Python:
		if kind == "module" || kind == "block" || kind == "class_definition" || kind == "decorated_definition" {
			return topLevel
		}
		return bodies
	case lang.Rust:
		switch kind {
		case "source_file":
			return unsplit
		case "impl_item", "mod_item", "trait_item", "declaration_list":
			return topLevel
		}
		return bodies
	case lang.TypeScript, lang.Tsx, lang.JavaScript:
		if kind == "program" {
			return unsplit
		}
		return topLevel
	}
	return unsplit
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
	switch l {
	case lang.Go:
		return goCuts(source, into)
	case lang.Rust:
		return rustCuts(source, into, depth == bodies)
	case lang.TypeScript, lang.Tsx, lang.JavaScript:
		return scriptCuts(source, into, l != lang.TypeScript)
	}
	lines := pythonLines{cuts: into, classes: depth == bodies, decorated: bytes.HasPrefix(source, []byte("@"))}
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
				if i = blockCommentEnd(source, i); i < 0 {
					return into, false
				}
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

type rustItem uint8

const (
	undecided rustItem = iota
	plainItem
	containerHead
	containerBody
	containerClosed
)

func rustCuts(source []byte, into []cut, bodies bool) ([]cut, bool) {
	var open []byte
	last, top, item, i := byte(0), 0, undecided, 0
	if bytes.HasPrefix(source, []byte("#!")) && !bytes.HasPrefix(source, []byte("#![")) {
		i = lineEnd(source, 0)
	}
	for ; i < len(source); i++ {
		switch c := source[i]; {
		case c == '\n':
			ended := (last == ';' || last == '}') && rustOpens(source, i+1)
			switch {
			case ended && len(open) == 0:
				into = append(into, cut{at: i + 1})
				top, item = len(into)-1, undecided
			case ended && len(open) == 1 && bodies && item == containerBody:
				into = append(into, cut{at: i + 1, inner: true})
			}
			continue
		case c == ' ' || c == '\t' || c == '\r':
			continue
		case c == '/' && i+1 < len(source) && source[i+1] == '/':
			i = lineEnd(source, i) - 1
			continue
		case c == '/' && i+1 < len(source) && source[i+1] == '*':
			if i = rustCommentEnd(source, i); i < 0 {
				return into, false
			}
			continue
		case c == '"':
			if i = rustStringEnd(source, i); i < 0 {
				return into, false
			}
		case c == '\'':
			if i = rustQuoteEnd(source, i); i < 0 {
				return into, false
			}
		case c >= utf8.RuneSelf:
			return into, false
		case identByte(c) || c == '$':
			word := wordAt(source, i)
			if raw := rawStringEnd(source, word, i+len(word)); raw != 0 {
				if i = raw; i < 0 {
					return into, false
				}
				break
			}
			if len(open) == 0 && item == undecided {
				item = rustItemOf(word)
			}
			i += len(word) - 1
		case c == '(' || c == '[' || c == '{':
			switch {
			case c == '{' && len(open) == 0 && item == containerHead:
				item = containerBody
			case c == '{' && len(open) == 0 && item == containerClosed:
				into, item = into[:top+1], plainItem
			}
			open = append(open, c)
		case c == ')' || c == ']' || c == '}':
			if len(open) == 0 || !closes(open[len(open)-1], c) {
				return into, false
			}
			if open = open[:len(open)-1]; len(open) == 0 && item == containerBody {
				item = containerClosed
			}
		}
		last = source[i]
	}
	return into, len(open) == 0
}

func rustItemOf(word []byte) rustItem {
	switch string(word) {
	case "pub", "unsafe":
		return undecided
	case "impl", "mod", "trait":
		return containerHead
	}
	return plainItem
}

func closes(opener, closer byte) bool {
	return (opener == '(' || opener == headParen) && closer == ')' || opener == '[' && closer == ']' || opener == '{' && closer == '}'
}

func rustOpens(source []byte, at int) bool {
	at = codeAfter(source, at, rustCommentEnd)
	if at < len(source) && source[at] == '#' {
		return at+1 < len(source) && source[at+1] == '['
	}
	switch string(wordAt(source, at)) {
	case "", "as", "else", "where":
		return false
	}
	return true
}

func codeAfter(source []byte, at int, commentEnd func([]byte, int) int) int {
	for at < len(source) {
		switch c := source[at]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v':
			at++
		case c == '/' && at+1 < len(source) && source[at+1] == '/':
			at = lineEnd(source, at)
		case c == '/' && at+1 < len(source) && source[at+1] == '*':
			if at = commentEnd(source, at); at < 0 {
				return len(source)
			}
			at++
		default:
			return at
		}
	}
	return at
}

func wordAt(source []byte, at int) []byte {
	end := at
	for end < len(source) && (identByte(source[end]) || source[end] == '$') {
		end++
	}
	return source[at:end]
}

func rustCommentEnd(source []byte, at int) int {
	depth := 0
	for j := at; j+1 < len(source); j++ {
		switch {
		case source[j] == '/' && source[j+1] == '*':
			depth++
			j++
		case source[j] == '*' && source[j+1] == '/':
			if j++; depth == 1 {
				return j
			}
			depth--
		}
	}
	return -1
}

func rustStringEnd(source []byte, at int) int {
	for j := at + 1; j < len(source); j++ {
		switch source[j] {
		case '\\':
			j++
		case '"':
			return j
		}
	}
	return -1
}

func rustQuoteEnd(source []byte, at int) int {
	if at+2 < len(source) && source[at+1] == '\\' {
		for j := at + 3; j < len(source) && source[j] != '\n'; j++ {
			if source[j] == '\'' {
				return j
			}
		}
		return -1
	}
	_, size := utf8.DecodeRune(source[at+1:])
	if end := at + 1 + size; end < len(source) && source[end] == '\'' && source[at+1] != '\n' {
		return end
	}
	return at
}

func rawStringEnd(source, word []byte, at int) int {
	if string(word) != "r" && string(word) != "br" && string(word) != "cr" {
		return 0
	}
	quote := at
	for quote < len(source) && source[quote] == '#' {
		quote++
	}
	if quote == len(source) || source[quote] != '"' {
		return 0
	}
	for j := quote + 1; j < len(source); j++ {
		if source[j] == '"' && bytes.HasPrefix(source[j+1:], source[at:quote]) {
			return j + quote - at
		}
	}
	return -1
}

const (
	headParen    byte = 'c'
	substitution byte = '$'
)

type expecting uint8

const (
	anOperand expecting = iota
	anOperator
	either
)

func scriptCuts(source []byte, into []cut, jsx bool) ([]cut, bool) {
	var open []byte
	next, last, head, i := anOperand, byte(0), false, 0
	if bytes.HasPrefix(source, []byte("#!")) {
		i = lineEnd(source, 0)
	}
	for ; i < len(source); i++ {
		switch c := source[i]; {
		case c == '\n':
			if len(open) == 0 && (last == ';' || last == '}') && scriptOpens(source, i+1) {
				into = append(into, cut{at: i + 1})
			}
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			continue
		case c == '/' && i+1 < len(source) && source[i+1] == '/':
			i = lineEnd(source, i) - 1
			continue
		case c == '/' && i+1 < len(source) && source[i+1] == '*':
			if i = blockCommentEnd(source, i); i < 0 {
				return into, false
			}
			continue
		case c >= utf8.RuneSelf:
			return into, false
		case identByte(c) || c == '$':
			word := wordAt(source, i)
			next, head = wordExpects(source, i, word), controls(word) || head && string(word) == "await"
			i += len(word) - 1
			last = source[i]
			continue
		case c == '/' && next == either, c == '<' && jsx && next != anOperator:
			return into, false
		case c == '/' && next == anOperand:
			if i = regexEnd(source, i); i < 0 {
				return into, false
			}
			next = anOperator
		case c == '\'' || c == '"':
			if i = quotedEnd(source, i, c); i < 0 {
				return into, false
			}
			next = anOperator
		case c == '`' || c == '}' && len(open) > 0 && open[len(open)-1] == substitution:
			if c == '}' {
				open = open[:len(open)-1]
			}
			end, substitutes := templateEnd(source, i+1)
			if end < 0 {
				return into, false
			}
			if next, i = anOperator, end; substitutes {
				next, open = anOperand, append(open, substitution)
			}
		case c == '(' && head:
			open, next = append(open, headParen), anOperand
		case c == '(' || c == '[' || c == '{':
			open, next = append(open, c), anOperand
		case c == ')' || c == ']' || c == '}':
			if len(open) == 0 || !closes(open[len(open)-1], c) {
				return into, false
			}
			opener := open[len(open)-1]
			open, next = open[:len(open)-1], anOperator
			switch {
			case opener == headParen:
				next = anOperand
			case c == '}':
				next = either
			}
		case (c == '+' || c == '-') && i+1 < len(source) && source[i+1] == c:
			i, next = i+1, either
		default:
			next = anOperand
		}
		head = false
		last = source[i]
	}
	return into, len(open) == 0
}

func controls(word []byte) bool {
	switch string(word) {
	case "if", "while", "for", "with":
		return true
	}
	return false
}

func wordExpects(source []byte, at int, word []byte) expecting {
	if digit(word[0]) || at > 0 && source[at-1] == '.' {
		return anOperator
	}
	switch string(word) {
	case "return", "typeof", "instanceof", "in", "new", "delete", "void", "throw", "case", "do", "else", "default", "extends":
		return anOperand
	case "await", "yield", "of", "let", "async", "static", "get", "set", "as", "satisfies":
		return either
	}
	return anOperator
}

func scriptOpens(source []byte, at int) bool {
	at = codeAfter(source, at, blockCommentEnd)
	if at < len(source) && source[at] == '@' {
		return true
	}
	switch string(wordAt(source, at)) {
	case "", "else", "while", "catch", "finally", "from", "in", "instanceof", "as", "satisfies", "extends", "implements", "of", "is", "with":
		return false
	}
	return true
}

func blockCommentEnd(source []byte, at int) int {
	if end := bytes.Index(source[at+2:], []byte("*/")); end >= 0 {
		return at + end + 3
	}
	return -1
}

func regexEnd(source []byte, at int) int {
	class := false
	for j := at + 1; j < len(source); j++ {
		switch source[j] {
		case '\\':
			if j+1 < len(source) && source[j+1] != '\n' && source[j+1] != '\r' {
				j++
			}
		case '[':
			class = true
		case ']':
			class = false
		case '/':
			if !class {
				return j
			}
		case '\n', '\r':
			return -1
		}
	}
	return -1
}

func templateEnd(source []byte, from int) (int, bool) {
	for j := from; j < len(source); j++ {
		switch source[j] {
		case '\\':
			j++
		case '`':
			return j, false
		case '$':
			if j+1 < len(source) && source[j+1] == '{' {
				return j + 1, true
			}
		}
	}
	return -1, false
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
