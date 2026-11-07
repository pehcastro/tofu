package lang

import (
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
)

type Lang uint8

const (
	Go Lang = iota + 1
	TypeScript
	Tsx
	Python
	Rust
)

const (
	metaVarChar          = '$'
	identifierStandIn    = 'µ'
	anonymousMultipleLen = 3
)

func (l Lang) String() string {
	switch l {
	case Go:
		return "go"
	case TypeScript:
		return "typescript"
	case Tsx:
		return "tsx"
	case Python:
		return "python"
	case Rust:
		return "rust"
	}
	panic("lang: unknown language")
}

func (l Lang) expando() rune {
	switch l {
	case Go, Python, Rust:
		return identifierStandIn
	case TypeScript, Tsx:
		return metaVarChar
	}
	panic("lang: unknown language")
}

func (l Lang) grammar() *gotreesitter.Language {
	switch l {
	case Go:
		return golang.Language()
	case TypeScript:
		return typescript.Language()
	case Tsx:
		return tsx.Language()
	case Python:
		return python.Language()
	case Rust:
		return rust.Language()
	}
	panic("lang: unknown language")
}

func FromPath(path string) (Lang, bool) {
	switch strings.TrimPrefix(filepath.Ext(path), ".") {
	case "go":
		return Go, true
	case "ts", "cts", "mts":
		return TypeScript, true
	case "tsx":
		return Tsx, true
	case "py", "py3", "pyi", "bzl", "bazel":
		return Python, true
	case "rs":
		return Rust, true
	}
	return 0, false
}

func (l Lang) PreprocessPattern(query string) string {
	expando := l.expando()
	if expando == metaVarChar {
		return query
	}
	var out strings.Builder
	dollars := 0
	for _, c := range query {
		if c == metaVarChar {
			dollars++
			continue
		}
		sigil := metaVarChar
		if c >= 'A' && c <= 'Z' || c == '_' || dollars == anonymousMultipleLen {
			sigil = expando
		}
		out.WriteString(strings.Repeat(string(sigil), dollars))
		dollars = 0
		out.WriteRune(c)
	}
	sigil := metaVarChar
	if dollars == anonymousMultipleLen {
		sigil = expando
	}
	out.WriteString(strings.Repeat(string(sigil), dollars))
	return out.String()
}
