//go:build !grammar_subset || grammar_subset_sql

package grammarruntime

import (
	"strings"
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const (
	sqlTokDollarStartTag = iota
	sqlTokDollarEndTag
	sqlTokDollarString
	sqlTokCopyData
	sqlTokGoSeparator
	sqlTokCustomDelimiter
	sqlTokImplicitEnd
	sqlTokenCount
)

var sqlExternals = []string{"_dollar_quoted_string_start_tag", "_dollar_quoted_string_end_tag", "_dollar_quoted_string", "copy_data", "_go_separator", "_custom_delimiter", "_implicit_end"}

type sqlScannerState struct {
	openTag string
}

type SqlExternalScanner struct {
	symbols         [sqlTokenCount]gotreesitter.Symbol
	externalToToken []int
}

func (s SqlExternalScanner) ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner {
	s.externalToToken = bindExternalScannerSymbolNames(lang, sqlExternals, func(tokenIdx int, sym gotreesitter.Symbol) {
		s.symbols[tokenIdx] = sym
	})
	return s
}

func (SqlExternalScanner) Create() any         { return &sqlScannerState{} }
func (SqlExternalScanner) Destroy(payload any) {}

func (SqlExternalScanner) Serialize(payload any, buf []byte) int {
	tag := payload.(*sqlScannerState).openTag
	if tag == "" || len(tag)+1 > len(buf) {
		return 0
	}
	copy(buf, tag)
	buf[len(tag)] = 0
	return len(tag) + 1
}

func (SqlExternalScanner) Deserialize(payload any, buf []byte) {
	state := payload.(*sqlScannerState)
	state.openTag = ""
	if len(buf) > 1 {
		state.openTag = string(buf[:len(buf)-1])
	}
}

func (s SqlExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	var valid [sqlTokenCount]bool
	for externalIdx, ok := range validSymbols {
		if ok && externalIdx < len(s.externalToToken) && s.externalToToken[externalIdx] >= 0 {
			valid[s.externalToToken[externalIdx]] = true
		}
	}
	state := payload.(*sqlScannerState)
	if (valid[sqlTokGoSeparator] || valid[sqlTokCustomDelimiter] || valid[sqlTokImplicitEnd]) && !valid[sqlTokDollarStartTag] {
		if tok, ok := scanSqlStatementEnd(lexer, valid); ok {
			lexer.SetResultSymbol(s.symbols[tok])
			return true
		}
	}
	switch {
	case valid[sqlTokCopyData] && !valid[sqlTokDollarStartTag]:
		lexer.SetResultSymbol(s.symbols[sqlTokCopyData])
		return scanSqlCopyData(lexer)
	case valid[sqlTokDollarStartTag] && state.openTag == "":
		skipSqlSpace(lexer)
		tag, ok := scanSqlDollarTag(lexer)
		if !ok {
			return false
		}
		state.openTag = tag
		lexer.MarkEnd()
		lexer.SetResultSymbol(s.symbols[sqlTokDollarStartTag])
		return true
	case valid[sqlTokDollarEndTag] && state.openTag != "":
		skipSqlSpace(lexer)
		tag, ok := scanSqlDollarTag(lexer)
		if !ok || tag != state.openTag {
			return false
		}
		state.openTag = ""
		lexer.MarkEnd()
		lexer.SetResultSymbol(s.symbols[sqlTokDollarEndTag])
		return true
	case valid[sqlTokDollarString]:
		return scanSqlDollarString(lexer, state.openTag, s.symbols[sqlTokDollarString])
	}
	return false
}

func scanSqlStatementEnd(lexer *gotreesitter.ExternalLexer, valid [sqlTokenCount]bool) (int, bool) {
	lineStart := lexer.Column() == 0
	for sqlSpace(lexer.Lookahead()) {
		lineStart = lineStart || lexer.Lookahead() == '\n'
		lexer.Advance(true)
	}
	c := lexer.Lookahead()
	if lineStart && (valid[sqlTokGoSeparator] || valid[sqlTokImplicitEnd]) && unicode.IsLetter(c) {
		lexer.MarkEnd()
		var word strings.Builder
		for c := lexer.Lookahead(); unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_'; c = lexer.Lookahead() {
			word.WriteRune(unicode.ToLower(c))
			lexer.Advance(false)
		}
		switch word.String() {
		case "go":
			if !valid[sqlTokGoSeparator] {
				return 0, false
			}
			lexer.MarkEnd()
			return sqlTokGoSeparator, true
		case "alter", "create", "drop", "insert", "update", "delete", "select", "set", "exec", "execute",
			"declare", "print", "use", "grant", "revoke", "if", "begin", "truncate", "merge", "commit":
			return sqlTokImplicitEnd, valid[sqlTokImplicitEnd]
		}
		return 0, false
	}
	if valid[sqlTokCustomDelimiter] && (c == '$' || c == ';' || c == '/') {
		lexer.Advance(false)
		if lexer.Lookahead() != c {
			return 0, false
		}
		lexer.Advance(false)
		lexer.MarkEnd()
		return sqlTokCustomDelimiter, true
	}
	return 0, false
}

func scanSqlDollarString(lexer *gotreesitter.ExternalLexer, openTag string, sym gotreesitter.Symbol) bool {
	lexer.MarkEnd()
	skipSqlSpace(lexer)
	start, ok := scanSqlDollarTag(lexer)
	if !ok || start == openTag {
		return false
	}
	for lexer.Lookahead() != 0 {
		end, ok := scanSqlDollarTag(lexer)
		if !ok {
			lexer.Advance(false)
			continue
		}
		if end == start {
			lexer.MarkEnd()
			lexer.SetResultSymbol(sym)
			return true
		}
	}
	return false
}

func scanSqlDollarTag(lexer *gotreesitter.ExternalLexer) (string, bool) {
	if lexer.Lookahead() != '$' {
		return "", false
	}
	tag := []rune{'$'}
	lexer.Advance(false)
	for c := lexer.Lookahead(); c != '$' && c != 0 && !sqlSpace(c); c = lexer.Lookahead() {
		tag = append(tag, c)
		lexer.Advance(false)
	}
	if lexer.Lookahead() != '$' {
		return "", false
	}
	lexer.Advance(false)
	return string(append(tag, '$')), true
}

func scanSqlCopyData(lexer *gotreesitter.ExternalLexer) bool {
	if !skipSqlLine(lexer) {
		return false
	}
	for lexer.Lookahead() != 0 {
		if lexer.Lookahead() == '\\' {
			lexer.Advance(false)
			if lexer.Lookahead() == '.' {
				lexer.Advance(false)
				if lexer.Lookahead() == '\r' {
					lexer.Advance(false)
				}
				if c := lexer.Lookahead(); c == '\n' || c == 0 {
					lexer.MarkEnd()
					return true
				}
			}
		}
		skipSqlLine(lexer)
	}
	lexer.MarkEnd()
	return true
}

func skipSqlLine(lexer *gotreesitter.ExternalLexer) bool {
	for lexer.Lookahead() != '\n' {
		if lexer.Lookahead() == 0 {
			return false
		}
		lexer.Advance(false)
	}
	lexer.Advance(false)
	return true
}

func skipSqlSpace(lexer *gotreesitter.ExternalLexer) {
	for sqlSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}
}

func sqlSpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}
