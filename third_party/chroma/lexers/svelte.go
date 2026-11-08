package lexers

import (
	. "github.com/alecthomas/chroma/v2"
)

func svelte() Lexer {
	return DelegatingLexer(html(), MustNewLexer(
		&Config{
			Name:      "Svelte",
			Aliases:   []string{"svelte"},
			Filenames: []string{"*.svelte"},
			MimeTypes: []string{"application/x-svelte"},
			DotAll:    true,
		},
		svelteRules,
	))
}

func svelteRules() Rules {
	return Rules{
		"root": {

			{`<!--`, Other, Push("comment")},
			{

				`(<\s*(?:script|style).*?lang\s*=\s*['"])` +
					`(.+?)(['"].*?>)` +
					`(.+?)` +
					`(<\s*/\s*(?:script|style)\s*>)`,
				UsingByGroup(2, 4, Other, Other, Other, Other, Other),
				nil,
			},
			{

				`(?<!<\s*(?:script|style)(?:(?!(?:script|style)\s*>).)*?)` +
					`{` +
					`(?!(?:(?!<\s*(?:script|style)).)*?(?:script|style)\s*>)`,
				Punctuation,
				Push("templates"),
			},

			{`(?<=\s+on:\w+(?:\|\w+)*)\|(?=\w+)`, Operator, nil},
			{`.+?`, Other, nil},
		},
		"comment": {
			{`-->`, Other, Pop(1)},
			{`.+?`, Other, nil},
		},
		"templates": {
			{`}`, Punctuation, Pop(1)},

			{`(?<!(?<!\\)\\)(['"` + "`])" + `.*?(?<!(?<!\\)\\)\1`, Using("TypeScript"), nil},

			{"{", Punctuation, Push("templates")},
			{`@(debug|html)\b`, Keyword, nil},
			{
				`(#await)(\s+)(\w+)(\s+)(then|catch)(\s+)(\w+)`,
				ByGroups(Keyword, Text, Using("TypeScript"), Text,
					Keyword, Text, Using("TypeScript"),
				),
				nil,
			},
			{`(#|/)(await|each|if|key)\b`, Keyword, nil},
			{`(:else)(\s+)(if)?\b`, ByGroups(Keyword, Text, Keyword), nil},
			{`:(catch|then)\b`, Keyword, nil},
			{`[^{}]+`, Using("TypeScript"), nil},
		},
	}
}
