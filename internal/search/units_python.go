package search

import (
	"regexp"
	"strings"
)

var (
	pythonDef   = regexp.MustCompile(`^(?:async\s+)?def\s+(\w+)`)
	pythonClass = regexp.MustCompile(`^class\s+(\w+)`)
)

type logicalLine struct {
	line   int
	indent int
	start  int
}

func pythonOutline(body string) (outline, bool) {
	o := newOutline(body)
	var logical []logicalLine
	depth, lineStart := 0, true
	for at := 0; at < len(body); {
		if lineStart {
			lineStart = false
			indent, first := 0, at
			for ; byteAt(body, first) == ' ' || byteAt(body, first) == '\t'; first++ {
				if body[first] == '\t' {
					indent += 8 - indent%8
				} else {
					indent++
				}
			}
			if first < len(body) && strings.IndexByte("\r\n#", body[first]) < 0 {
				logical = append(logical, logicalLine{line: o.lineOf(first), indent: indent, start: first})
			}
			at = first
			continue
		}
		end, as := at+1, regionCode
		switch body[at] {
		case '#':
			end, as = lineEnd(body, at), regionComment
		case '\'', '"':
			end, as = pythonStringEnd(body, at), regionString
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\\':
			end = at + 2
			if byteAt(body, at+1) == '\r' {
				end++
			}
		case '\n':
			lineStart = depth == 0
		}
		if end < 0 || depth < 0 {
			return outline{}, false
		}
		end = min(end, len(body))
		o.mark(at, end, as)
		at = end
	}
	if depth != 0 {
		return outline{}, false
	}
	for index, opens := range logical {
		text := body[opens.start:lineEnd(body, opens.start)]
		decl := block{kind: KindFunc, first: opens.line}
		if match := pythonDef.FindStringSubmatch(text); match != nil {
			decl.symbol = match[1]
		} else if match := pythonClass.FindStringSubmatch(text); match != nil {
			decl.kind, decl.symbol, decl.container = KindType, match[1], true
		} else {
			continue
		}
		for above := index - 1; above >= 0 && logical[above].indent == opens.indent && body[logical[above].start] == '@'; above-- {
			decl.first = logical[above].line
		}
		decl.last = len(o.lineStarts)
		for _, after := range logical[index+1:] {
			if after.indent <= opens.indent {
				decl.last = after.line - 1
				break
			}
		}
		for decl.last > opens.line && o.blankLine(body, decl.last) {
			decl.last--
		}
		o.blocks = append(o.blocks, decl)
	}
	return o, true
}

func (o outline) blankLine(body string, line int) bool {
	text := strings.TrimLeft(body[o.lineStarts[line-1]:], " \t\r")
	return text == "" || text[0] == '\n' || o.regions[len(body)-len(text)] == regionComment
}

func pythonStringEnd(body string, at int) int {
	closing := body[at : at+1]
	if strings.HasPrefix(body[at:], strings.Repeat(closing, 3)) {
		closing = strings.Repeat(closing, 3)
	}
	for scan := at + len(closing); scan < len(body); scan++ {
		switch {
		case body[scan] == '\\':
			scan++
		case strings.HasPrefix(body[scan:], closing):
			return scan + len(closing)
		case body[scan] == '\n' && len(closing) == 1:
			return -1
		}
	}
	return -1
}
