package scanner

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type tokenType int

func (t tokenType) String() string {
	return tokenNames[t]
}

type Token struct {
	Type   tokenType
	Value  string
	Line   int
	Column int
}

func (t *Token) String() string {
	if len(t.Value) > 10 {
		return fmt.Sprintf("%s (line: %d, column: %d): %.10q...",
			t.Type, t.Line, t.Column, t.Value)
	}
	return fmt.Sprintf("%s (line: %d, column: %d): %q",
		t.Type, t.Line, t.Column, t.Value)
}

const (
	TokenError tokenType = iota
	TokenEOF

	TokenIdent
	TokenAtKeyword
	TokenString
	TokenHash
	TokenNumber
	TokenPercentage
	TokenDimension
	TokenURI
	TokenUnicodeRange
	TokenCDO
	TokenCDC
	TokenS
	TokenComment
	TokenFunction
	TokenIncludes
	TokenDashMatch
	TokenPrefixMatch
	TokenSuffixMatch
	TokenSubstringMatch
	TokenChar
	TokenBOM
)

var tokenNames = map[tokenType]string{
	TokenError:          "error",
	TokenEOF:            "EOF",
	TokenIdent:          "IDENT",
	TokenAtKeyword:      "ATKEYWORD",
	TokenString:         "STRING",
	TokenHash:           "HASH",
	TokenNumber:         "NUMBER",
	TokenPercentage:     "PERCENTAGE",
	TokenDimension:      "DIMENSION",
	TokenURI:            "URI",
	TokenUnicodeRange:   "UNICODE-RANGE",
	TokenCDO:            "CDO",
	TokenCDC:            "CDC",
	TokenS:              "S",
	TokenComment:        "COMMENT",
	TokenFunction:       "FUNCTION",
	TokenIncludes:       "INCLUDES",
	TokenDashMatch:      "DASHMATCH",
	TokenPrefixMatch:    "PREFIXMATCH",
	TokenSuffixMatch:    "SUFFIXMATCH",
	TokenSubstringMatch: "SUBSTRINGMATCH",
	TokenChar:           "CHAR",
	TokenBOM:            "BOM",
}

var matchers = sync.OnceValue(func() map[tokenType]*regexp.Regexp {
	macroRegexp := regexp.MustCompile(`\{[a-z]+\}`)
	replaceMacro := func(s string) string {
		return "(?:" + macros[s[1:len(s)-1]] + ")"
	}
	compiled := map[tokenType]*regexp.Regexp{}
	for t, s := range productions {
		for macroRegexp.MatchString(s) {
			s = macroRegexp.ReplaceAllStringFunc(s, replaceMacro)
		}
		compiled[t] = regexp.MustCompile("^(?:" + s + ")")
	}
	return compiled
})

var macros = map[string]string{
	"ident":      `-?{nmstart}{nmchar}*`,
	"name":       `{nmchar}+`,
	"nmstart":    `[a-zA-Z_]|{nonascii}|{escape}`,
	"nonascii":   "[\u0080-\uD7FF\uE000-\uFFFD\U00010000-\U0010FFFF]",
	"unicode":    `\\[0-9a-fA-F]{1,6}{wc}?`,
	"escape":     "{unicode}|\\\\[\u0020-\u007E\u0080-\uD7FF\uE000-\uFFFD\U00010000-\U0010FFFF]",
	"nmchar":     `[a-zA-Z0-9_-]|{nonascii}|{escape}`,
	"num":        `[0-9]*\.[0-9]+|[0-9]+`,
	"string":     `"(?:{stringchar}|')*"|'(?:{stringchar}|")*'`,
	"stringchar": `{urlchar}|[ ]|\\{nl}`,
	"nl":         `[\n\r\f]|\r\n`,
	"w":          `{wc}*`,
	"wc":         `[\t\n\f\r ]`,
	"urlchar":    "[\u0021\u0023-\u0026\u0028-\\\u005b\\\u005d-\u007E]|{nonascii}|{escape}",
}

var productions = map[tokenType]string{
	TokenIdent:        `{ident}`,
	TokenAtKeyword:    `@{ident}`,
	TokenString:       `{string}`,
	TokenHash:         `#{name}`,
	TokenNumber:       `{num}`,
	TokenPercentage:   `{num}%`,
	TokenDimension:    `{num}{ident}`,
	TokenURI:          `url\({w}(?:{string}|{urlchar}*?){w}\)`,
	TokenUnicodeRange: `U\+[0-9A-F\?]{1,6}(?:-[0-9A-F]{1,6})?`,
	TokenCDC:          `-->`,
	TokenS:            `{wc}+`,
	TokenComment:      `/\*[^\*]*[\*]+(?:[^/][^\*]*[\*]+)*/`,
	TokenFunction:     `{ident}\(`,
}

var matchOrder = []tokenType{
	TokenURI,
	TokenFunction,
	TokenUnicodeRange,
	TokenIdent,
	TokenDimension,
	TokenPercentage,
	TokenNumber,
	TokenCDC,
}

func New(input string) *Scanner {

	input = strings.Replace(input, "\r\n", "\n", -1)
	input = strings.Replace(input, "\r", "\n", -1)
	input = strings.Replace(input, "\f", "\n", -1)
	input = strings.Replace(input, "\u0000", "\ufffd", -1)
	return &Scanner{
		input: input,
		row:   1,
		col:   1,
	}
}

type Scanner struct {
	input string
	pos   int
	row   int
	col   int
	err   *Token
}

func (s *Scanner) Next() *Token {
	if s.err != nil {
		return s.err
	}
	if s.pos >= len(s.input) {
		s.err = &Token{TokenEOF, "", s.row, s.col}
		return s.err
	}
	if s.pos == 0 {

		if strings.HasPrefix(s.input, "\uFEFF") {
			return s.emitSimple(TokenBOM, "\uFEFF")
		}
	}

	input := s.input[s.pos:]
	switch input[0] {
	case '\t', '\n', ' ':

		return s.emitToken(TokenS, matchers()[TokenS].FindString(input))
	case '.':

		if len(input) > 1 && !unicode.IsDigit(rune(input[1])) {
			return s.emitSimple(TokenChar, ".")
		}
	case '#':

		if match := matchers()[TokenHash].FindString(input); match != "" {
			return s.emitToken(TokenHash, match)
		}
		return s.emitSimple(TokenChar, "#")
	case '@':

		if match := matchers()[TokenAtKeyword].FindString(input); match != "" {
			return s.emitSimple(TokenAtKeyword, match)
		}
		return s.emitSimple(TokenChar, "@")
	case ':', ',', ';', '%', '&', '+', '=', '>', '(', ')', '[', ']', '{', '}':

		return s.emitSimple(TokenChar, string(input[0]))
	case '"', '\'':

		match := matchers()[TokenString].FindString(input)
		if match != "" {
			return s.emitToken(TokenString, match)
		}

		s.err = &Token{TokenError, "unclosed quotation mark", s.row, s.col}
		return s.err
	case '/':

		if len(input) > 1 && input[1] == '*' {
			match := matchers()[TokenComment].FindString(input)
			if match != "" {
				return s.emitToken(TokenComment, match)
			} else {
				s.err = &Token{TokenError, "unclosed comment", s.row, s.col}
				return s.err
			}
		}
		return s.emitSimple(TokenChar, "/")
	case '~':

		return s.emitPrefixOrChar(TokenIncludes, "~=")
	case '|':

		return s.emitPrefixOrChar(TokenDashMatch, "|=")
	case '^':

		return s.emitPrefixOrChar(TokenPrefixMatch, "^=")
	case '$':

		return s.emitPrefixOrChar(TokenSuffixMatch, "$=")
	case '*':

		return s.emitPrefixOrChar(TokenSubstringMatch, "*=")
	case '<':

		return s.emitPrefixOrChar(TokenCDO, "<!--")
	}

	for _, token := range matchOrder {
		if match := matchers()[token].FindString(input); match != "" {
			return s.emitToken(token, match)
		}
	}

	r, width := utf8.DecodeRuneInString(input)
	token := &Token{TokenChar, string(r), s.row, s.col}
	s.col += width
	s.pos += width
	return token
}

func (s *Scanner) updatePosition(text string) {
	width := utf8.RuneCountInString(text)
	lines := strings.Count(text, "\n")
	s.row += lines
	if lines == 0 {
		s.col += width
	} else {
		s.col = utf8.RuneCountInString(text[strings.LastIndex(text, "\n"):])
	}
	s.pos += len(text)
}

func (s *Scanner) emitToken(t tokenType, v string) *Token {
	token := &Token{t, v, s.row, s.col}
	s.updatePosition(v)
	return token
}

func (s *Scanner) emitSimple(t tokenType, v string) *Token {
	token := &Token{t, v, s.row, s.col}
	s.col += len(v)
	s.pos += len(v)
	return token
}

func (s *Scanner) emitPrefixOrChar(t tokenType, prefix string) *Token {
	if strings.HasPrefix(s.input[s.pos:], prefix) {
		return s.emitSimple(t, prefix)
	}
	return s.emitSimple(TokenChar, string(prefix[0]))
}
