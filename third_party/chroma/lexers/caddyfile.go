package lexers

import (
	. "github.com/alecthomas/chroma/v2"
)

var caddyfileMatcherTokenRegexp = `(\[\<matcher\>\]|@[^\s]+|/[^\s]+|\*)`

var caddyfileCommentRegexp = `(^|\s+)#.*\n`

func caddyfileCommonRules() Rules {
	return Rules{
		"site_block_common": {
			Include("site_body"),

			{`[^\s#]+`, Keyword, Push("directive")},
			Include("base"),
		},
		"site_body": {

			{`\b(import|invoke)\b( [^\s#]+)`, ByGroups(Keyword, Text), Push("subdirective")},

			{`@[^\s]+(?=\s)`, NameDecorator, Push("matcher")},

			{`\[\<matcher\>\]`, NameDecorator, Push("matcher")},

			{`\b(try_files|tls|log|bind)\b`, Keyword, Push("subdirective")},

			{`\b(handle_errors|handle_path|handle_response|replace_status|handle|route)\b`, Keyword, Push("nested_directive")},

			{`\b(uri)\b`, Keyword, Push("uri_directive")},
		},
		"matcher": {
			{`\{`, Punctuation, Push("block")},

			{`not`, Keyword, Push("deep_not_matcher")},

			Include("heredoc"),

			{"`", StringBacktick, Push("backticks")},

			{`[^\s#]+`, Keyword, Push("arguments")},

			{`\s*\n`, Text, Pop(1)},
			{`\}`, Punctuation, Pop(1)},
			Include("base"),
		},
		"block": {
			{`\}`, Punctuation, Pop(2)},

			{`"`, StringDouble, Push("double_quotes")},

			{"`", StringBacktick, Push("backticks")},

			{`not`, Keyword, Push("not_matcher")},

			Include("site_body"),

			{`[^\s#]+`, Keyword, Push("subdirective")},
			Include("base"),
		},
		"nested_block": {
			{`\}`, Punctuation, Pop(2)},

			{`"`, StringDouble, Push("double_quotes")},

			{"`", StringBacktick, Push("backticks")},

			{`not`, Keyword, Push("not_matcher")},

			Include("site_body"),

			{`[^\s#]+`, Keyword, Push("directive")},
			Include("base"),
		},
		"not_matcher": {
			{`\}`, Punctuation, Pop(2)},
			{`\{(?=\s)`, Punctuation, Push("block")},
			{`[^\s#]+`, Keyword, Push("arguments")},
			{`\s+`, Text, nil},
		},
		"deep_not_matcher": {
			{`\}`, Punctuation, Pop(2)},
			{`\{(?=\s)`, Punctuation, Push("block")},
			{`[^\s#]+`, Keyword, Push("deep_subdirective")},
			{`\s+`, Text, nil},
		},
		"directive": {
			{`\{(?=\s)`, Punctuation, Push("block")},
			{caddyfileMatcherTokenRegexp, NameDecorator, Push("arguments")},
			{caddyfileCommentRegexp, CommentSingle, Pop(1)},
			{`\s*\n`, Text, Pop(1)},
			Include("base"),
		},
		"nested_directive": {
			{`\{(?=\s)`, Punctuation, Push("nested_block")},
			{caddyfileMatcherTokenRegexp, NameDecorator, Push("nested_arguments")},
			{caddyfileCommentRegexp, CommentSingle, Pop(1)},
			{`\s*\n`, Text, Pop(1)},
			Include("base"),
		},
		"subdirective": {
			{`\{(?=\s)`, Punctuation, Push("block")},
			{caddyfileCommentRegexp, CommentSingle, Pop(1)},
			{`\s*\n`, Text, Pop(1)},
			Include("base"),
		},
		"arguments": {
			{`\{(?=\s)`, Punctuation, Push("block")},
			{caddyfileCommentRegexp, CommentSingle, Pop(2)},
			{`\\\n`, Text, nil},
			{`\s*\n`, Text, Pop(2)},
			Include("base"),
		},
		"nested_arguments": {
			{`\{(?=\s)`, Punctuation, Push("nested_block")},
			{caddyfileCommentRegexp, CommentSingle, Pop(2)},
			{`\\\n`, Text, nil},
			{`\s*\n`, Text, Pop(2)},
			Include("base"),
		},
		"deep_subdirective": {
			{`\{(?=\s)`, Punctuation, Push("block")},
			{caddyfileCommentRegexp, CommentSingle, Pop(3)},
			{`\s*\n`, Text, Pop(3)},
			Include("base"),
		},
		"uri_directive": {
			{`\{(?=\s)`, Punctuation, Push("block")},
			{caddyfileMatcherTokenRegexp, NameDecorator, nil},
			{`(strip_prefix|strip_suffix|replace|path_regexp)`, NameConstant, Push("arguments")},
			{caddyfileCommentRegexp, CommentSingle, Pop(1)},
			{`\s*\n`, Text, Pop(1)},
			Include("base"),
		},
		"double_quotes": {
			Include("placeholder"),
			{`\\"`, StringDouble, nil},
			{`[^"]`, StringDouble, nil},
			{`"`, StringDouble, Pop(1)},
		},
		"backticks": {
			Include("placeholder"),
			{"\\\\`", StringBacktick, nil},
			{"[^`]", StringBacktick, nil},
			{"`", StringBacktick, Pop(1)},
		},
		"optional": {

			{`\[`, Punctuation, Push("optional")},
			Include("name_constants"),
			{`\|`, Punctuation, nil},
			{`[^\[\]\|]+`, String, nil},
			{`\]`, Punctuation, Pop(1)},
		},
		"heredoc": {
			{`(<<([a-zA-Z0-9_-]+))(\n(.*|\n)*)(\s*)(\2)`, ByGroups(StringHeredoc, nil, String, String, String, StringHeredoc), nil},
		},
		"name_constants": {
			{`\b(most_recently_modified|largest_size|smallest_size|first_exist|internal|disable_redirects|ignore_loaded_certs|disable_certs|private_ranges|first|last|before|after|on|off)\b(\||(?=\]|\s|$))`, ByGroups(NameConstant, Punctuation), nil},
		},
		"placeholder": {

			{`\{[\w+.\[\]\:\$-]+\}`, StringEscape, nil},

			{`\{[^\}\s]*\b`, String, nil},
		},
		"base": {
			{caddyfileCommentRegexp, CommentSingle, nil},
			{`\[\<matcher\>\]`, NameDecorator, nil},
			Include("name_constants"),
			Include("heredoc"),
			{`(https?://)?([a-z0-9.-]+)(:)([0-9]+)([^\s]*)`, ByGroups(Name, Name, Punctuation, NumberInteger, Name), nil},
			{`\[`, Punctuation, Push("optional")},
			{"`", StringBacktick, Push("backticks")},
			{`"`, StringDouble, Push("double_quotes")},
			Include("placeholder"),
			{`[a-z-]+/[a-z-+]+`, String, nil},
			{`[0-9]+([smhdk]|ns|us|µs|ms)?\b`, NumberInteger, nil},
			{`[^\s\n#\{]+`, String, nil},
			{`/[^\s#]*`, Name, nil},
			{`\s+`, Text, nil},
		},
	}
}

func caddyfile() Lexer {
	return MustNewLexer(
		&Config{
			Name:      "Caddyfile",
			Aliases:   []string{"caddyfile", "caddy"},
			Filenames: []string{"Caddyfile*"},
			MimeTypes: []string{},
		},
		caddyfileRules,
	)
}

func caddyfileRules() Rules {
	return Rules{
		"root": {
			{caddyfileCommentRegexp, CommentSingle, nil},

			{`^\s*(\{)\s*$`, ByGroups(Punctuation), Push("globals")},

			{`(import)(\s+)([^\s]+)`, ByGroups(Keyword, Text, NameVariableMagic), nil},

			{`(&?\([^\s#]+\))(\s*)(\{)`, ByGroups(NameVariableAnonymous, Text, Punctuation), Push("snippet")},

			{`[^#{(\s,]+`, GenericHeading, Push("label")},

			{`\{[\w+.\[\]\:\$-]+\}`, StringEscape, Push("label")},
			{`\s+`, Text, nil},
		},
		"globals": {
			{`\}`, Punctuation, Pop(1)},

			{`[^\s#]+`, Keyword, Push("subdirective")},
			Include("base"),
		},
		"snippet": {
			{`\}`, Punctuation, Pop(1)},
			Include("site_body"),

			{`[^\s#]+`, Keyword, Push("directive")},
			Include("base"),
		},
		"label": {

			{`,\s*\n?`, Text, nil},
			{` `, Text, nil},

			Include("placeholder"),

			{`[^#{(\s,]+`, GenericHeading, nil},

			{`#.*\n`, CommentSingle, Push("site_block")},

			{`\{(?=\s)|\n`, Punctuation, Push("site_block")},
		},
		"site_block": {
			{`\}`, Punctuation, Pop(2)},
			Include("site_block_common"),
		},
	}.Merge(caddyfileCommonRules())
}

func caddyfileDirectives() Lexer {
	return MustNewLexer(
		&Config{
			Name:      "Caddyfile Directives",
			Aliases:   []string{"caddyfile-directives", "caddyfile-d", "caddy-d"},
			Filenames: []string{},
			MimeTypes: []string{},
		},
		caddyfileDirectivesRules,
	)
}

func caddyfileDirectivesRules() Rules {
	return Rules{

		"root": {
			Include("site_block_common"),
		},
	}.Merge(caddyfileCommonRules())
}
