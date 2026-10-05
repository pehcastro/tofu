package search

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type declPattern struct {
	shape     *regexp.Regexp
	kind      Kind
	container bool
	named     func(string) string
}

type braceSyntax struct {
	rust    bool
	decls   []declPattern
	members []declPattern
	calls   []declPattern
	stops   *regexp.Regexp
}

const (
	scriptArrow  = `=\s*(?:async\b\s*)?(?:function\b.*|(?:<.*>\s*)?(?:\(.*\)|[\w$]+)\s*(?::.*)?=>)$`
	rustVisible  = `^(?:pub(?:\s*\([^)]*\))?\s+)?`
	scriptMember = `^(?:@[\w$.]+(?:\(.*?\))?\s+)*`
)

var (
	scriptSyntax = braceSyntax{
		decls: []declPattern{
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:async\s+)?function\b\s*\*?\s*([\w$]*)`), kind: KindFunc},
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?class\b\s*([\w$]*)`), kind: KindType, container: true},
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:declare\s+)?(?:const\s+)?(?:interface|enum)\s+([\w$]+)`), kind: KindType},
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:declare\s+)?type\s+([\w$]+)`), kind: KindType},
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:const|let|var)\s+([\w$]+)[^=]*` + scriptArrow), kind: KindFunc},
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:const|let|var)\s+([\w$]+)[^=]*=\s*$`), kind: KindValue},
		},
		members: []declPattern{
			{shape: regexp.MustCompile(`(?s)` + scriptMember + `(?:(?:public|private|protected|static|readonly|async|override|abstract|declare|accessor|get|set)\s+)*\*?\s*(#?[\w$]+)\s*\??\s*(?:<.*>)?\s*\(.*\)\s*(?::.*)?$`), kind: KindFunc},
			{shape: regexp.MustCompile(`(?s)` + scriptMember + `(?:(?:public|private|protected|static|readonly|override)\s+)*(#?[\w$]+)\s*(?::[^=]*)?` + scriptArrow), kind: KindFunc},
		},
		calls: []declPattern{
			{shape: regexp.MustCompile(`(?s)^(?:export\s+)?(?:const|let|var)\s+([\w$]+)[^=]*=\s*(?:await\s+)?[\w$.]+(?:<.*>)?$`), kind: KindValue},
		},
		stops: regexp.MustCompile(`^(?:abstract|async|await|class|const|declare|do|else|enum|export|for|function|if|import|interface|let|new|return|switch|throw|try|type|var|while|yield)\b`),
	}
	rustSyntax = braceSyntax{
		rust: true,
		decls: []declPattern{
			{shape: regexp.MustCompile(`(?s)` + rustVisible + `(?:default\s+)?(?:const\s+)?(?:async\s+)?(?:unsafe\s+)?(?:extern\s+(?:"[^"]*"\s+)?)?fn\s+(\w+)`), kind: KindFunc},
			{shape: regexp.MustCompile(`(?s)` + rustVisible + `(?:struct|enum|union)\s+(\w+)`), kind: KindType},
			{shape: regexp.MustCompile(`(?s)` + rustVisible + `(?:unsafe\s+)?(?:auto\s+)?trait\s+(\w+)`), kind: KindType, container: true},
			{shape: regexp.MustCompile(`(?s)^(?:unsafe\s+)?impl\b(.*)$`), kind: KindType, container: true, named: implTarget},
		},
		stops: regexp.MustCompile(`^(?:async|const|else|enum|extern|fn|for|if|impl|let|loop|match|mod|pub|return|static|struct|trait|type|union|unsafe|use|while)\b`),
	}
	implType     = regexp.MustCompile(`^&?\s*(?:'\w+\s+)?(?:(?:mut|dyn)\s+)*(?:\w+::)*(\w+)`)
	decoratorRun = regexp.MustCompile(`(?s)^(?:(?:@[\w$.]+\s*(?:\(.*\))?|#!?\[.*\])\s*)+$`)
)

type frame struct {
	open     byte
	header   int
	template bool
	declared bool
	decl     block
}

func braceOutline(body string, syntax braceSyntax) (outline, bool) {
	o := newOutline(body)
	stack := []frame{{open: '{'}}
	resumeTemplate := func(from int) int {
		end, interpolates := templateEnd(body, from)
		if interpolates {
			stack = append(stack, frame{open: '{', template: true, header: end})
		}
		return end
	}
	previous := -1
	for at := 0; at < len(body); {
		c, next := body[at], byteAt(body, at+1)
		end, as := at+1, regionCode
		switch {
		case c == '/' && next == '/':
			end, as = lineEnd(body, at), regionComment
		case c == '/' && next == '*':
			end, as = blockCommentEnd(body, at, syntax.rust), regionComment
		case c == '"' || (c == '\'' && !syntax.rust && !wordInside(body, at)):
			end, as = quotedEnd(body, at, !syntax.rust), regionString
		case c == '\'' && syntax.rust:
			end = charLiteralEnd(body, at)
			if end > at+1 {
				as = regionString
			}
		case syntax.rust && (c == 'r' || c == 'b') && !identByte(byteAt(body, at-1)):
			if raw := rawStringEnd(body, at); raw > 0 {
				end, as = raw, regionString
			}
		case c == '`' && !syntax.rust:
			end, as = resumeTemplate(at+1), regionString
		case c == '/' && !syntax.rust && regexAllowed(body, previous):
			if closed := regexEnd(body, at); closed > 0 {
				end, as = closed, regionString
			}
		case c == '{' || c == '(' || c == '[':
			opened := frame{open: c, header: at + 1}
			if top := stack[len(stack)-1]; top.open == '{' && c != '[' {
				patterns := syntax.decls
				switch {
				case c == '(':
					patterns = syntax.calls
				case top.declared && top.decl.container && syntax.members != nil:
					patterns = syntax.members
				}
				if len(patterns) > 0 {
					opened.decl, opened.declared = o.classify(body, top.header, at, patterns, syntax.stops)
				}
			}
			stack = append(stack, opened)
		case c == '}' || c == ')' || c == ']':
			closed := stack[len(stack)-1]
			if len(stack) == 1 || closed.open != "{(["[strings.IndexByte("})]", c)] {
				return outline{}, false
			}
			stack = stack[:len(stack)-1]
			switch {
			case closed.template:
				end, as = resumeTemplate(at+1), regionString
			case closed.declared:
				closed.decl.last = o.lineOf(at)
				o.blocks = append(o.blocks, closed.decl)
			}
			if c == '}' {
				stack[len(stack)-1].header = max(end, at+1)
			}
		case c == ';':
			stack[len(stack)-1].header = at + 1
		}
		if end < 0 {
			return outline{}, false
		}
		if as != regionCode {
			o.mark(at, end, as)
		} else if c > ' ' {
			previous = at
		}
		at = end
	}
	return o, len(stack) == 1
}

func (o outline) classify(body string, from, to int, patterns []declPattern, stops *regexp.Regexp) (block, bool) {
	var starts []int
	depth := 0
	for at := from; at < to; at++ {
		if depth == 0 && (at == from || body[at-1] == '\n') {
			first := to - len(strings.TrimLeft(body[at:to], " \t\r"))
			if first < to && body[first] != '\n' && o.regions[first] == regionCode {
				starts = append(starts, first)
			}
		}
		if o.regions[at] != regionCode {
			continue
		}
		switch body[at] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	for index := len(starts) - 1; index >= 0; index-- {
		text := strings.TrimSpace(body[starts[index]:to])
		for _, pattern := range patterns {
			match := pattern.shape.FindStringSubmatch(text)
			if match == nil {
				continue
			}
			decl := block{kind: pattern.kind, symbol: match[1], first: o.lineOf(starts[index]), container: pattern.container}
			if pattern.named != nil {
				decl.symbol = pattern.named(match[1])
			}
			for above := index - 1; above >= 0 && decoratorRun.MatchString(strings.TrimSpace(body[starts[above]:starts[above+1]])); above-- {
				decl.first = o.lineOf(starts[above])
			}
			return decl, true
		}
		if stops.MatchString(text) {
			break
		}
	}
	return block{}, false
}

func lineEnd(body string, at int) int {
	if next := strings.IndexByte(body[at:], '\n'); next >= 0 {
		return at + next
	}
	return len(body)
}

func byteAt(body string, at int) byte {
	if at < 0 || at >= len(body) {
		return 0
	}
	return body[at]
}

func identByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func wordInside(body string, at int) bool {
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	return letter(byteAt(body, at-1)) && letter(byteAt(body, at+1))
}

func quotedEnd(body string, at int, endsAtLine bool) int {
	quote := body[at]
	for scan := at + 1; scan < len(body); scan++ {
		switch body[scan] {
		case '\\':
			scan++
		case quote:
			return scan + 1
		case '\n':
			if endsAtLine {
				return scan
			}
		}
	}
	return -1
}

func templateEnd(body string, from int) (int, bool) {
	for scan := from; scan < len(body); scan++ {
		switch {
		case body[scan] == '\\':
			scan++
		case body[scan] == '`':
			return scan + 1, false
		case body[scan] == '$' && byteAt(body, scan+1) == '{':
			return scan + 2, true
		}
	}
	return -1, false
}

func blockCommentEnd(body string, at int, nests bool) int {
	depth := 0
	for scan := at; scan+1 < len(body); scan++ {
		switch body[scan : scan+2] {
		case "/*":
			if depth == 0 || nests {
				depth++
			}
			scan++
		case "*/":
			depth--
			scan++
			if depth == 0 {
				return scan + 1
			}
		}
	}
	return -1
}

func charLiteralEnd(body string, at int) int {
	if byteAt(body, at+1) == '\\' {
		if closing := strings.IndexByte(body[min(at+3, len(body)):], '\''); closing >= 0 {
			return at + 3 + closing + 1
		}
		return -1
	}
	_, width := utf8.DecodeRuneInString(body[min(at+1, len(body)):])
	if width > 0 && byteAt(body, at+1+width) == '\'' {
		return at + 2 + width
	}
	return at + 1
}

func rawStringEnd(body string, at int) int {
	scan := at
	if body[scan] == 'b' {
		scan++
	}
	if byteAt(body, scan) != 'r' {
		return 0
	}
	scan++
	hashes := 0
	for byteAt(body, scan) == '#' {
		hashes++
		scan++
	}
	if byteAt(body, scan) != '"' {
		return 0
	}
	closing := strings.Index(body[scan+1:], "\""+strings.Repeat("#", hashes))
	if closing < 0 {
		return -1
	}
	return scan + 1 + closing + 1 + hashes
}

func regexAllowed(body string, previous int) bool {
	if previous < 0 {
		return true
	}
	if identByte(body[previous]) {
		return strings.HasSuffix(body[:previous+1], "return") && !identByte(byteAt(body, previous-len("return")))
	}
	return strings.IndexByte("(,=:[!&|?{;", body[previous]) >= 0 || strings.HasSuffix(body[:previous+1], "=>")
}

func regexEnd(body string, at int) int {
	inClass := false
	for scan := at + 1; scan < len(body); scan++ {
		switch body[scan] {
		case '\\':
			scan++
		case '\n':
			return 0
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if inClass {
				continue
			}
			scan++
			for scan < len(body) && identByte(body[scan]) {
				scan++
			}
			return scan
		}
	}
	return 0
}

func implTarget(rest string) string {
	fields := strings.Fields(rest)
	rest = strings.Join(fields, " ")
	for index := len(fields) - 2; index >= 0; index-- {
		if fields[index] == "for" {
			rest = strings.Join(fields[index+1:], " ")
			break
		}
	}
	if strings.HasPrefix(rest, "<") {
		depth := 0
		for index, c := range rest {
			switch c {
			case '<':
				depth++
			case '>':
				depth--
			}
			if depth == 0 {
				rest = strings.TrimSpace(rest[index+1:])
				break
			}
		}
	}
	if match := implType.FindStringSubmatch(rest); match != nil {
		return match[1]
	}
	return ""
}
