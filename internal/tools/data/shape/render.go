package shape

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type language int

const (
	golang language = iota
	typescript
	rust
	python
)

func (l language) String() string {
	switch l {
	case golang:
		return "go"
	case typescript:
		return "ts"
	case rust:
		return "rust"
	case python:
		return "python"
	}
	return "language " + strconv.Itoa(int(l))
}

type spelling struct {
	boolean, integer, float, text, unknown, union string
}

func (l language) spelling() spelling {
	switch l {
	case golang:
		return spelling{"bool", "int64", "float64", "string", "any", "any"}
	case typescript:
		return spelling{"boolean", "number", "number", "string", "unknown", ""}
	case rust:
		return spelling{"bool", "i64", "f64", "String", "serde_json::Value", "serde_json::Value"}
	}
	return spelling{"bool", "int", "float", "str", "Any", ""}
}

type declared struct {
	name  string
	shape *shape
}

type renderer struct {
	lang  language
	names map[string]bool
	queue []declared
}

func (r *renderer) typeOf(s *shape, name string) string {
	return r.joined(r.alternatives(s, name))
}

func (r *renderer) alternatives(s *shape, name string) []string {
	words := r.lang.spelling()
	var parts []string
	for _, k := range []kind{kindBool, kindInt, kindFloat, kindString, kindObject, kindArray} {
		if s.kinds&k == 0 || k == kindInt && s.kinds&kindFloat != 0 {
			continue
		}
		switch k {
		case kindBool:
			parts = append(parts, words.boolean)
		case kindInt:
			parts = append(parts, words.integer)
		case kindFloat:
			parts = append(parts, words.float)
		case kindString:
			parts = append(parts, words.text)
		case kindObject:
			parts = append(parts, r.declare(name, s))
		case kindArray:
			parts = append(parts, r.list(r.typeOf(s.items, singular(name))))
		}
	}
	return parts
}

func (r *renderer) joined(parts []string) string {
	words := r.lang.spelling()
	switch {
	case len(parts) == 0:
		return words.unknown
	case len(parts) == 1:
		return parts[0]
	case words.union != "":
		return words.union
	}
	return strings.Join(parts, " | ")
}

func (r *renderer) list(item string) string {
	switch r.lang {
	case golang:
		return "[]" + item
	case typescript:
		if strings.Contains(item, " | ") {
			return "(" + item + ")[]"
		}
		return item + "[]"
	case rust:
		return "Vec<" + item + ">"
	}
	return "list[" + item + "]"
}

func (r *renderer) declare(want string, s *shape) string {
	name := want
	for n := 2; r.names[name]; n++ {
		name = want + strconv.Itoa(n)
	}
	r.names[name] = true
	r.queue = append(r.queue, declared{name, s})
	return name
}

type member struct {
	key, typed        string
	missing, nullable bool
}

func (r *renderer) render() string {
	var blocks, marked []string
	for i := 0; i < len(r.queue); i++ {
		d := r.queue[i]
		var members []member
		for _, f := range d.shape.fields {
			parts := r.alternatives(&f.shape, pascal(f.key))
			m := member{key: f.key, typed: r.joined(parts), missing: f.present < d.shape.objects, nullable: f.nulls > 0}
			at := "  " + d.name + "." + f.key
			if m.missing {
				first := cmp.Or(f.firstMissing, f.seenAt+1)
				marked = append(marked, at+" optional: missing from "+strconv.Itoa(d.shape.objects-f.present)+" of "+strconv.Itoa(d.shape.objects)+", first in item "+strconv.Itoa(first))
			}
			if m.nullable {
				marked = append(marked, at+" optional: null in "+strconv.Itoa(f.nulls)+" of "+strconv.Itoa(d.shape.objects)+", first in item "+strconv.Itoa(f.firstNull))
			}
			if len(parts) > 1 {
				note := at + " union: " + strings.Join(parts, " | ")
				if r.lang.spelling().union != "" {
					note += ", typed " + m.typed
				}
				marked = append(marked, note)
			}
			members = append(members, m)
		}
		blocks = append(blocks, r.declaration(d.name, members))
	}
	if r.lang == python {
		slices.Reverse(blocks)
	}
	out := strings.Join(blocks, "\n")
	switch r.lang {
	case rust:
		out = "use serde::{Deserialize, Serialize};\n\n" + out
	case python:
		out = "from typing import Any, NotRequired, TypedDict\n\n" + out
	case golang, typescript:
	}
	if len(marked) > 0 {
		out += "\nmarked:\n" + strings.Join(marked, "\n") + "\n"
	}
	return out
}

func (r *renderer) declaration(name string, members []member) string {
	switch r.lang {
	case golang:
		return goStruct(name, members)
	case typescript:
		return tsInterface(name, members)
	case rust:
		return rustStruct(name, members)
	}
	return pythonDict(name, members)
}

func goStruct(name string, members []member) string {
	used := map[string]bool{}
	rows := make([][3]string, 0, len(members))
	widths := [2]int{}
	for _, m := range members {
		ident, typed, tag := unique(pascal(m.key), used), m.typed, m.key
		if m.missing || m.nullable {
			tag += ",omitempty"
			if !strings.HasPrefix(typed, "[]") && typed != "any" {
				typed = "*" + typed
			}
		}
		rows = append(rows, [3]string{ident, typed, "`json:" + strconv.Quote(tag) + "`"})
		widths = [2]int{max(widths[0], len(ident)), max(widths[1], len(typed))}
	}
	out := "type " + name + " struct {\n"
	for _, row := range rows {
		out += "\t" + row[0] + strings.Repeat(" ", widths[0]-len(row[0])+1) + row[1] + strings.Repeat(" ", widths[1]-len(row[1])+1) + row[2] + "\n"
	}
	return out + "}\n"
}

func tsInterface(name string, members []member) string {
	out := "export interface " + name + " {\n"
	for _, m := range members {
		key, mark, typed := m.key, "", m.typed
		if !identifier(key, "$") {
			key = strconv.Quote(key)
		}
		if m.missing || m.nullable {
			mark = "?"
		}
		if m.nullable && typed != "unknown" {
			typed += " | null"
		}
		out += "  " + key + mark + ": " + typed + ";\n"
	}
	return out + "}\n"
}

func rustStruct(name string, members []member) string {
	used := map[string]bool{}
	out := "#[derive(Debug, Clone, Serialize, Deserialize)]\npub struct " + name + " {\n"
	for _, m := range members {
		ident, typed := unique(snake(m.key), used), m.typed
		if ident != m.key {
			out += "    #[serde(rename = " + strconv.Quote(m.key) + ")]\n"
		}
		if m.missing || m.nullable {
			typed = "Option<" + typed + ">"
		}
		out += "    pub " + ident + ": " + typed + ",\n"
	}
	return out + "}\n"
}

func pythonDict(name string, members []member) string {
	classy := !slices.ContainsFunc(members, func(m member) bool { return !identifier(m.key, "") || pythonKeyword(m.key) })
	out := name + " = TypedDict(" + strconv.Quote(name) + ", {\n"
	if classy {
		out = "class " + name + "(TypedDict):\n"
	}
	for _, m := range members {
		typed := m.typed
		if m.nullable && typed != "Any" {
			typed += " | None"
		}
		if m.missing {
			typed = "NotRequired[" + typed + "]"
		}
		if classy {
			out += "    " + m.key + ": " + typed + "\n"
		} else {
			out += "    " + strconv.Quote(m.key) + ": " + typed + ",\n"
		}
	}
	switch {
	case !classy:
		return out + "})\n"
	case len(members) == 0:
		return out + "    pass\n"
	}
	return out
}

func unique(want string, used map[string]bool) string {
	name := want
	for n := 2; used[name]; n++ {
		name = want + strconv.Itoa(n)
	}
	used[name] = true
	return name
}

func pascal(key string) string {
	var b strings.Builder
	for _, word := range strings.FieldsFunc(key, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if upper := strings.ToUpper(word); slices.Contains([]string{"ID", "URL", "API", "HTTP", "JSON", "UUID", "IP"}, upper) {
			b.WriteString(upper)
			continue
		}
		first, size := utf8.DecodeRuneInString(word)
		b.WriteString(string(unicode.ToUpper(first)) + word[size:])
	}
	name := b.String()
	if first, _ := utf8.DecodeRuneInString(name); name == "" || unicode.IsDigit(first) {
		name = "Field" + name
	}
	return name
}

func singular(name string) string {
	switch {
	case strings.HasSuffix(name, "ies"):
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss"):
		return strings.TrimSuffix(name, "s")
	}
	return name + "Item"
}

func snake(key string) string {
	var b strings.Builder
	previous := rune(0)
	for _, r := range key {
		switch {
		case unicode.IsUpper(r):
			if unicode.IsLower(previous) || unicode.IsDigit(previous) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case !strings.HasSuffix(b.String(), "_"):
			b.WriteByte('_')
		}
		previous = r
	}
	name := strings.Trim(b.String(), "_")
	if first, _ := utf8.DecodeRuneInString(name); name == "" || unicode.IsDigit(first) {
		name = strings.TrimSuffix("field_"+name, "_")
	}
	if slices.Contains([]string{"as", "async", "await", "break", "const", "continue", "crate", "dyn", "else", "enum", "extern", "false", "fn", "for", "if", "impl", "in", "let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return", "self", "static", "struct", "super", "trait", "true", "type", "unsafe", "use", "where", "while"}, name) {
		name += "_"
	}
	return name
}

func identifier(key, extra string) bool {
	for i, r := range key {
		if !unicode.IsLetter(r) && r != '_' && !strings.ContainsRune(extra, r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return key != ""
}

func pythonKeyword(key string) bool {
	return slices.Contains([]string{"False", "None", "True", "and", "as", "assert", "async", "await", "break", "class", "continue", "def", "del", "elif", "else", "except", "finally", "for", "from", "global", "if", "import", "in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try", "while", "with", "yield"}, key)
}
