package lexers

import (
	"embed"
	"io/fs"
	"sync"

	"github.com/alecthomas/chroma/v2"
)

//go:embed embedded
var embedded embed.FS

var (
	GlobalLexerRegistry = chroma.NewLexerRegistry()
	registryOnce        sync.Once
)

func registry() *chroma.LexerRegistry {
	registryOnce.Do(func() {
		paths, err := fs.Glob(embedded, "embedded/*.xml")
		if err != nil {
			panic(err)
		}
		for _, path := range paths {
			GlobalLexerRegistry.Register(chroma.MustNewXMLLexer(embedded, path))
		}
		for _, build := range []func() chroma.Lexer{
			caddyfile, caddyfileDirectives, commonLisp, emacsLisp, erb, gemtext, genshiText, genshiHTMLTemplate, genshi,
			goLexer, goHTMLTemplate, goTextTemplate, haxe, httpLexer, markdown, markless, php, raku, restructuredtext, svelte, typoscript,
		} {
			GlobalLexerRegistry.Register(build())
		}
		analyseDNS()
		analyseMySQL()
		analyseZed()
	})
	return GlobalLexerRegistry
}

func html() chroma.Lexer { return GlobalLexerRegistry.Get("HTML") }

func Names(withAliases bool) []string {
	return registry().Names(withAliases)
}

func Aliases(skipWithoutAliases bool) []string {
	return registry().Aliases(skipWithoutAliases)
}

func Get(name string) chroma.Lexer {
	return registry().Get(name)
}

func MatchMimeType(mimeType string) chroma.Lexer {
	return registry().MatchMimeType(mimeType)
}

func Match(filename string) chroma.Lexer {
	return registry().Match(filename)
}

func Register(lexer chroma.Lexer) chroma.Lexer {
	return registry().Register(lexer)
}

func Analyse(text string) chroma.Lexer {
	return registry().Analyse(text)
}

func PlaintextRules() chroma.Rules {
	return chroma.Rules{
		"root": []chroma.Rule{
			{`.+`, chroma.Text, nil},
			{`\n`, chroma.Text, nil},
		},
	}
}

var Fallback chroma.Lexer = chroma.MustNewLexer(&chroma.Config{
	Name:      "fallback",
	Filenames: []string{"*"},
	Priority:  -1,
}, PlaintextRules)
