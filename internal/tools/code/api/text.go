package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type syntax struct {
	top         string
	namesIndent string
	container   string
	member      string
	header      *regexp.Regexp
	end         func(text string, start int, header bool) int
	body        func(text string, start int) (int, int)
	doc         func(text string, start, end int) string
}

func tsSyntax() syntax {
	return syntax{
		top:         `(?m)^%[1]s(?:export[ \t]+)?(?:declare[ \t]+)?(?:default[ \t]+)?(?:abstract[ \t]+)?(?:function|class|interface|type|const|let|var|enum|namespace|module)[ \t]+(?P<name>%[2]s)\b`,
		namesIndent: `[ \t]*`,
		container:   `(?m)^[ \t]*(?:export[ \t]+)?(?:declare[ \t]+)?(?:default[ \t]+)?(?:abstract[ \t]+)?(?:class|interface|namespace|module|enum)[ \t]+%[1]s\b`,
		member:      `(?m)^[ \t]*(?:(?:readonly|static|public|protected|abstract|declare|get|set|async|function|const|let|var|export|class|interface|type)[ \t]+)*(?P<name>%[1]s)[ \t]*[?!]?[ \t]*[(<:=;{]`,
		end:         braceEnd,
		body:        braceBody,
		doc:         jsDoc,
	}
}

func rustSyntax() syntax {
	return syntax{
		top:         `(?m)^%[1]s(?:pub[ \t]+(?:(?:const|async|unsafe|extern[ \t]+"[^"]*")[ \t]+)*(?:fn|struct|enum|trait|type|const|static|union|mod)|macro_rules!)[ \t]+(?P<name>%[2]s)\b`,
		namesIndent: ``,
		container:   `(?m)^[ \t]*(?:pub[ \t]+trait[ \t]+%[1]s\b|impl\b[^{;]*\b%[1]s\b)`,
		member:      `(?m)^[ \t]*(?:pub(?:\([^)]*\))?[ \t]+)?(?:(?:const|async|unsafe|default|extern[ \t]+"[^"]*")[ \t]+)*(?:fn|type|const)[ \t]+(?P<name>%[1]s)\b`,
		header:      regexp.MustCompile(`\b(?:fn|mod)[ \t]`),
		end:         braceEnd,
		body:        braceBody,
		doc:         rustDoc,
	}
}

func pythonSyntax() syntax {
	return syntax{
		top:         `(?m)^%[1]s(?:async[ \t]+)?(?:def|class)[ \t]+(?P<name>%[2]s)\b`,
		namesIndent: ``,
		container:   `(?m)^class[ \t]+%[1]s\b`,
		member:      `(?m)^[ \t]+(?:async[ \t]+)?(?:def|class)[ \t]+(?P<name>%[1]s)\b`,
		end:         pythonEnd,
		body:        pythonBody,
		doc:         pythonDoc,
	}
}

func (s syntax) scan(inst installed, segments []string) (answer, error) {
	found := answer{installed: inst}
	names := regexp.MustCompile(fmt.Sprintf(s.top, s.namesIndent, `[A-Za-z_$][\w$]*`))
	var want, within *regexp.Regexp
	switch len(segments) {
	case 1:
		want = regexp.MustCompile(fmt.Sprintf(s.top, `[ \t]*`, regexp.QuoteMeta(segments[0])))
	case 2:
		within = regexp.MustCompile(fmt.Sprintf(s.container, regexp.QuoteMeta(segments[0])))
		want = regexp.MustCompile(fmt.Sprintf(s.member, regexp.QuoteMeta(segments[1])))
	}
	seen := map[string]bool{}
	for _, path := range inst.files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return answer{}, err
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		for _, match := range names.FindAllStringSubmatch(text, -1) {
			if name := match[names.SubexpIndex("name")]; !seen[name] && !strings.HasPrefix(name, "_") {
				seen[name] = true
				found.names = append(found.names, name)
			}
		}
		if want == nil {
			continue
		}
		spans := [][2]int{{0, len(text)}}
		if within != nil {
			spans = nil
			for _, at := range within.FindAllStringIndex(text, -1) {
				from, to := s.body(text, at[0])
				spans = append(spans, [2]int{from, to})
			}
		}
		rel, _ := filepath.Rel(inst.dir, path)
		for _, span := range spans {
			for _, at := range want.FindAllStringIndex(text[span[0]:span[1]], -1) {
				start := span[0] + at[0]
				end := s.end(text, start, s.header != nil && s.header.MatchString(text[start:span[0]+at[1]]))
				found.decls = append(found.decls, declaration{
					file:      filepath.ToSlash(rel),
					line:      strings.Count(text[:start], "\n") + 1,
					signature: dedent(text[start:end]),
					doc:       s.doc(text, start, end),
				})
			}
		}
	}
	return found, nil
}

func braceEnd(text string, start int, header bool) int {
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '(', '[':
			depth++
		case '{':
			if header && depth == 0 {
				return i
			}
			depth++
		case ')', ']':
			depth--
		case '}':
			if depth--; depth == 0 {
				return i + 1
			}
		case ';':
			if depth == 0 {
				return i + 1
			}
		}
		if depth < 0 {
			return i
		}
	}
	return len(text)
}

func braceBody(text string, start int) (int, int) {
	open := braceEnd(text, start, true)
	if open >= len(text) || text[open] != '{' {
		return open, open
	}
	return open + 1, braceEnd(text, open, false) - 1
}

func pythonEnd(text string, start int, _ bool) int {
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ':':
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(text)
}

func pythonBody(text string, start int) (int, int) {
	indent := len(text[start:]) - len(strings.TrimLeft(text[start:], " \t"))
	from := pythonEnd(text, start, true)
	at := from
	for at < len(text) {
		line, _, _ := strings.Cut(text[at:], "\n")
		if trimmed := strings.TrimLeft(line, " \t"); trimmed != "" && len(line)-len(trimmed) <= indent {
			break
		}
		at += len(line) + 1
	}
	return from, min(at, len(text))
}

func jsDoc(text string, start, _ int) string {
	before := strings.TrimRight(text[:start], " \t\n")
	open := strings.LastIndex(before, "/**")
	if !strings.HasSuffix(before, "*/") || open < 0 || strings.Contains(before[open:len(before)-2], "*/") {
		return ""
	}
	lines := strings.Split(before[open+3:len(before)-2], "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(line), "*"), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func rustDoc(text string, start, _ int) string {
	lines := strings.Split(text[:start], "\n")
	var doc []string
	for i := len(lines) - 2; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#[") {
			continue
		}
		if !strings.HasPrefix(line, "///") {
			break
		}
		doc = append([]string{strings.TrimPrefix(strings.TrimPrefix(line, "///"), " ")}, doc...)
	}
	return strings.TrimSpace(strings.Join(doc, "\n"))
}

func pythonDoc(text string, _, end int) string {
	rest := strings.TrimLeft(strings.TrimLeft(text[end:], " \t\n"), "rRuU")
	for _, quote := range []string{`"""`, `'''`} {
		if body, _, closed := strings.Cut(strings.TrimPrefix(rest, quote), quote); strings.HasPrefix(rest, quote) && closed {
			lines := strings.Split(body, "\n")
			for i, line := range lines {
				lines[i] = strings.TrimSpace(line)
			}
			return strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return ""
}

func dedent(text string) string {
	indent := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, indent)
	}
	return strings.Join(lines, "\n")
}
