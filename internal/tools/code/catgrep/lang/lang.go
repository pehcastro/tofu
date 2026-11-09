package lang

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/bash"
	"github.com/odvcencio/gotreesitter/grammars/c"
	csharp "github.com/odvcencio/gotreesitter/grammars/c_sharp"
	"github.com/odvcencio/gotreesitter/grammars/clojure"
	"github.com/odvcencio/gotreesitter/grammars/cmake"
	"github.com/odvcencio/gotreesitter/grammars/cpp"
	"github.com/odvcencio/gotreesitter/grammars/css"
	"github.com/odvcencio/gotreesitter/grammars/dart"
	"github.com/odvcencio/gotreesitter/grammars/dockerfile"
	"github.com/odvcencio/gotreesitter/grammars/elixir"
	"github.com/odvcencio/gotreesitter/grammars/erlang"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	"github.com/odvcencio/gotreesitter/grammars/groovy"
	"github.com/odvcencio/gotreesitter/grammars/haskell"
	"github.com/odvcencio/gotreesitter/grammars/hcl"
	"github.com/odvcencio/gotreesitter/grammars/html"
	"github.com/odvcencio/gotreesitter/grammars/java"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/json"
	"github.com/odvcencio/gotreesitter/grammars/julia"
	"github.com/odvcencio/gotreesitter/grammars/kotlin"
	"github.com/odvcencio/gotreesitter/grammars/less"
	"github.com/odvcencio/gotreesitter/grammars/lua"
	makefile "github.com/odvcencio/gotreesitter/grammars/make"
	"github.com/odvcencio/gotreesitter/grammars/markdown"
	"github.com/odvcencio/gotreesitter/grammars/nix"
	"github.com/odvcencio/gotreesitter/grammars/objc"
	"github.com/odvcencio/gotreesitter/grammars/ocaml"
	"github.com/odvcencio/gotreesitter/grammars/php"
	"github.com/odvcencio/gotreesitter/grammars/powershell"
	"github.com/odvcencio/gotreesitter/grammars/proto"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/r"
	"github.com/odvcencio/gotreesitter/grammars/ruby"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/scala"
	"github.com/odvcencio/gotreesitter/grammars/scss"
	"github.com/odvcencio/gotreesitter/grammars/solidity"
	"github.com/odvcencio/gotreesitter/grammars/sql"
	"github.com/odvcencio/gotreesitter/grammars/svelte"
	"github.com/odvcencio/gotreesitter/grammars/swift"
	"github.com/odvcencio/gotreesitter/grammars/toml"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
	"github.com/odvcencio/gotreesitter/grammars/vue"
	"github.com/odvcencio/gotreesitter/grammars/yaml"
	"github.com/odvcencio/gotreesitter/grammars/zig"
)

type Lang uint8

const (
	Go Lang = iota + 1
	TypeScript
	Tsx
	Python
	Rust
	JavaScript
	Php
	Svelte
	Vue
	Html
	Css
	Scss
	Less
	C
	Cpp
	CSharp
	Java
	Kotlin
	Swift
	Ruby
	Lua
	Bash
	Elixir
	Scala
	Dart
	Haskell
	Zig
	Sql
	Yaml
	Json
	Toml
	Markdown
	OCaml
	Erlang
	R
	Julia
	Nix
	Hcl
	Dockerfile
	Make
	CMake
	PowerShell
	ObjC
	Groovy
	Proto
	Solidity
	Clojure
)

const (
	metaVarChar          = '$'
	identifierStandIn    = 'µ'
	underscoreStandIn    = '_'
	tagStandIn           = 'z'
	cStandIn             = '𐀀'
	anonymousMultipleLen = 3
	phpOpenTag           = "<?php "
)

type role uint8

const (
	plain role = iota
	scriptHost
	embeddable
)

type spec struct {
	name       string
	aliases    []string
	extensions []string
	filenames  []string
	grammar    func() *gotreesitter.Language
	expando    rune
	role       role
}

var languages = [...]spec{
	Go:         {"go", []string{"golang"}, []string{"go"}, nil, golang.Language, identifierStandIn, plain},
	TypeScript: {"typescript", []string{"ts"}, []string{"ts", "cts", "mts"}, nil, typescript.Language, metaVarChar, embeddable},
	Tsx:        {"tsx", nil, []string{"tsx"}, nil, tsx.Language, metaVarChar, embeddable},
	Python:     {"python", []string{"py"}, []string{"py", "py3", "pyi", "bzl", "bazel"}, nil, python.Language, identifierStandIn, plain},
	Rust:       {"rust", []string{"rs"}, []string{"rs"}, nil, rust.Language, identifierStandIn, plain},
	JavaScript: {"javascript", []string{"js", "jsx", "react"}, []string{"js", "mjs", "cjs", "jsx"}, nil, javascript.Language, metaVarChar, embeddable},
	Php:        {"php", nil, []string{"php", "phtml"}, nil, php.Language, identifierStandIn, plain},
	Svelte:     {"svelte", nil, []string{"svelte"}, nil, svelte.Language, tagStandIn, scriptHost},
	Vue:        {"vue", nil, []string{"vue"}, nil, vue.Language, tagStandIn, scriptHost},
	Html:       {"html", []string{"htm"}, []string{"html", "htm", "xhtml"}, nil, html.Language, tagStandIn, scriptHost},
	Css:        {"css", nil, []string{"css"}, nil, css.Language, underscoreStandIn, embeddable},
	Scss:       {"scss", []string{"sass"}, []string{"scss"}, nil, scss.Language, underscoreStandIn, embeddable},
	Less:       {"less", nil, []string{"less"}, nil, less.Language, underscoreStandIn, embeddable},
	C:          {"c", nil, []string{"c", "h"}, nil, c.Language, cStandIn, plain},
	Cpp:        {"cpp", []string{"c++", "cc", "cxx"}, []string{"cc", "cpp", "cxx", "c++", "hpp", "hh", "hxx", "ino"}, nil, cpp.Language, cStandIn, plain},
	CSharp:     {"csharp", []string{"cs", "c#"}, []string{"cs"}, nil, csharp.Language, identifierStandIn, plain},
	Java:       {"java", nil, []string{"java"}, nil, java.Language, metaVarChar, plain},
	Kotlin:     {"kotlin", []string{"kt"}, []string{"kt", "kts", "ktm"}, nil, kotlin.Language, identifierStandIn, plain},
	Swift:      {"swift", nil, []string{"swift"}, nil, swift.Language, identifierStandIn, plain},
	Ruby:       {"ruby", []string{"rb"}, []string{"rb", "rbw", "rake", "gemspec"}, []string{"Rakefile", "Gemfile"}, ruby.Language, identifierStandIn, plain},
	Lua:        {"lua", nil, []string{"lua"}, nil, lua.Language, metaVarChar, plain},
	Bash:       {"bash", []string{"sh", "shell"}, []string{"sh", "bash", "bats"}, nil, bash.Language, metaVarChar, plain},
	Elixir:     {"elixir", []string{"ex"}, []string{"ex", "exs"}, nil, elixir.Language, identifierStandIn, plain},
	Scala:      {"scala", nil, []string{"scala", "sc", "sbt"}, nil, scala.Language, metaVarChar, plain},
	Dart:       {"dart", nil, []string{"dart"}, nil, dart.Language, metaVarChar, plain},
	Haskell:    {"haskell", []string{"hs"}, []string{"hs"}, nil, haskell.Language, identifierStandIn, plain},
	Zig:        {"zig", nil, []string{"zig"}, nil, zig.Language, underscoreStandIn, plain},
	Sql:        {"sql", nil, []string{"sql"}, nil, sql.Language, underscoreStandIn, plain},
	Yaml:       {"yaml", []string{"yml"}, []string{"yaml", "yml"}, nil, yaml.Language, metaVarChar, plain},
	Json:       {"json", nil, []string{"json"}, nil, json.Language, metaVarChar, plain},
	Toml:       {"toml", nil, []string{"toml"}, nil, toml.Language, underscoreStandIn, plain},
	Markdown:   {"markdown", []string{"md"}, []string{"md", "markdown"}, nil, markdown.Language, metaVarChar, plain},
	OCaml:      {"ocaml", []string{"ml"}, []string{"ml"}, nil, ocaml.Language, underscoreStandIn, plain},
	Erlang:     {"erlang", []string{"erl"}, []string{"erl", "hrl"}, nil, erlang.Language, underscoreStandIn, plain},
	R:          {"r", nil, []string{"r"}, nil, r.Language, identifierStandIn, plain},
	Julia:      {"julia", []string{"jl"}, []string{"jl"}, nil, julia.Language, identifierStandIn, plain},
	Nix:        {"nix", nil, []string{"nix"}, nil, nix.Language, underscoreStandIn, plain},
	Hcl:        {"hcl", []string{"terraform", "tf"}, []string{"hcl", "tf", "tfvars", "nomad"}, nil, hcl.Language, identifierStandIn, plain},
	Dockerfile: {"dockerfile", []string{"docker"}, []string{"dockerfile"}, []string{"Dockerfile", "Containerfile"}, dockerfile.Language, metaVarChar, plain},
	Make:       {"make", []string{"makefile"}, []string{"mk", "mak"}, []string{"Makefile", "GNUmakefile", "makefile"}, makefile.Language, metaVarChar, plain},
	CMake:      {"cmake", nil, []string{"cmake"}, []string{"CMakeLists.txt"}, cmake.Language, metaVarChar, plain},
	PowerShell: {"powershell", []string{"ps1", "pwsh"}, []string{"ps1", "psm1", "psd1"}, nil, powershell.Language, metaVarChar, plain},
	ObjC:       {"objc", []string{"objective-c"}, []string{"m", "mm"}, nil, objc.Language, cStandIn, plain},
	Groovy:     {"groovy", []string{"gradle"}, []string{"groovy", "gradle", "gvy"}, []string{"Jenkinsfile"}, groovy.Language, metaVarChar, plain},
	Proto:      {"proto", []string{"protobuf"}, []string{"proto"}, nil, proto.Language, underscoreStandIn, plain},
	Solidity:   {"solidity", []string{"sol"}, []string{"sol"}, nil, solidity.Language, metaVarChar, plain},
	Clojure:    {"clojure", []string{"clj"}, []string{"clj", "cljs", "cljc", "edn"}, nil, clojure.Language, metaVarChar, plain},
}

func (l Lang) spec() spec {
	if l == 0 || int(l) >= len(languages) {
		panic("lang: unknown language")
	}
	return languages[l]
}

func (l Lang) String() string { return l.spec().name }

func (l Lang) Grammar() *gotreesitter.Language { return l.spec().grammar() }

func (l Lang) HostsScripts() bool { return l.spec().role == scriptHost }

func (l Lang) Embeddable() bool { return l.spec().role == embeddable }

func Named(name string) (Lang, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for l := Lang(1); int(l) < len(languages); l++ {
		if languages[l].name == name || slices.Contains(languages[l].aliases, name) {
			return l, true
		}
	}
	return 0, false
}

func Listing() string {
	names := make([]string, 0, len(languages)-1)
	for _, s := range languages[1:] {
		if len(s.aliases) == 0 {
			names = append(names, s.name)
			continue
		}
		names = append(names, s.name+" ("+strings.Join(s.aliases, ", ")+")")
	}
	return strings.Join(names, ", ")
}

func FromPath(path string) (Lang, bool) {
	base := filepath.Base(path)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))
	for l := Lang(1); int(l) < len(languages); l++ {
		if slices.Contains(languages[l].filenames, base) || ext != "" && slices.Contains(languages[l].extensions, ext) {
			return l, true
		}
	}
	return 0, false
}

func (l Lang) Sigil() rune { return l.spec().expando }

func (l Lang) PreprocessPattern(query string) string {
	var out strings.Builder
	if l == Php {
		out.WriteString(phpOpenTag)
	}
	expando := l.Sigil()
	if expando == metaVarChar {
		out.WriteString(query)
		return out.String()
	}
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
