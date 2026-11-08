//go:build !grammar_subset || grammar_subset_groovy

package grammarruntime

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const (
	groovySlashyStart = iota
	groovyLineComment
	groovyBlockComment
	groovyDocComment
	groovyCommandStart
	groovyRecordKeyword
	groovyVarKeyword
	groovyLambdaOpen
	groovyTupleAssignOpen
	groovyStringBody
	groovyExternalCount
)

type GroovyExternalScanner struct {
	symbols [groovyExternalCount]gotreesitter.Symbol
}

func (GroovyExternalScanner) ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner {
	var s GroovyExternalScanner
	copy(s.symbols[:], lang.ExternalSymbols)
	return s
}

func (GroovyExternalScanner) Create() any                       { return nil }
func (GroovyExternalScanner) Destroy(any)                       {}
func (GroovyExternalScanner) Serialize(any, []byte) int         { return 0 }
func (GroovyExternalScanner) Deserialize(any, []byte)           {}
func (GroovyExternalScanner) ExternalScannerIsStateless() bool  { return true }
func (GroovyExternalScanner) PreservesStateOnScanFailure() bool { return true }

func (s GroovyExternalScanner) Scan(_ any, lexer *gotreesitter.ExternalLexer, valid []bool) bool {
	if valid[groovyStringBody] && !valid[groovyCommandStart] {
		return false
	}
	wantsComment := valid[groovyLineComment] || valid[groovyBlockComment] || valid[groovyDocComment]
	wantsWord := valid[groovyCommandStart] || valid[groovyRecordKeyword] || valid[groovyVarKeyword]
	wantsParen := valid[groovyLambdaOpen] || valid[groovyTupleAssignOpen]
	if !wantsComment && !valid[groovySlashyStart] && !wantsWord && !wantsParen {
		return false
	}
	newline := false
	for groovySpace(lexer.Lookahead()) {
		newline = newline || lexer.Lookahead() == '\n' || lexer.Lookahead() == '\r'
		lexer.Advance(true)
	}
	switch lexer.Lookahead() {
	case '(':
		return wantsParen && s.paren(lexer, valid)
	case '/':
		return s.slash(lexer, valid, wantsComment)
	}
	return wantsWord && s.word(lexer, valid, newline)
}

func (s GroovyExternalScanner) slash(lexer *gotreesitter.ExternalLexer, valid []bool, wantsComment bool) bool {
	lexer.Advance(false)
	switch lexer.Lookahead() {
	case '/':
		for lexer.Lookahead() != 0 && lexer.Lookahead() != '\n' {
			lexer.Advance(false)
		}
		return valid[groovyLineComment] && s.emit(lexer, groovyLineComment)
	case '*':
		return wantsComment && s.blockComment(lexer, valid)
	case '=', ' ', '\t', '\n', '\r':
		return false
	}
	if !valid[groovySlashyStart] {
		return false
	}
	lexer.MarkEnd()
	for {
		switch lexer.Lookahead() {
		case 0:
			return false
		case '/':
			return s.emit(lexer, groovySlashyStart)
		case '\\':
			lexer.Advance(false)
		}
		lexer.Advance(false)
	}
}

func (s GroovyExternalScanner) blockComment(lexer *gotreesitter.ExternalLexer, valid []bool) bool {
	lexer.Advance(false)
	kind := groovyBlockComment
	if lexer.Lookahead() == '*' {
		lexer.Advance(false)
		kind = groovyDocComment
		if lexer.Lookahead() == '/' {
			lexer.Advance(false)
			return valid[kind] && s.emit(lexer, kind)
		}
	}
	for lexer.Lookahead() != 0 {
		star := lexer.Lookahead() == '*'
		lexer.Advance(false)
		if star && lexer.Lookahead() == '/' {
			lexer.Advance(false)
			return valid[kind] && s.emit(lexer, kind)
		}
	}
	return false
}

func (s GroovyExternalScanner) paren(lexer *gotreesitter.ExternalLexer, valid []bool) bool {
	lexer.Advance(false)
	lexer.MarkEnd()
	for depth := 1; depth > 0; lexer.Advance(false) {
		switch lexer.Lookahead() {
		case 0:
			return false
		case '(':
			depth++
		case ')':
			depth--
		case '\'', '"':
			if !groovySkipQuoted(lexer) {
				return false
			}
		}
	}
	groovySkipSpace(lexer)
	switch lexer.Lookahead() {
	case '-':
		lexer.Advance(false)
		return lexer.Lookahead() == '>' && valid[groovyLambdaOpen] && s.emit(lexer, groovyLambdaOpen)
	case '=':
		lexer.Advance(false)
		c := lexer.Lookahead()
		return c != '=' && c != '~' && valid[groovyTupleAssignOpen] && s.emit(lexer, groovyTupleAssignOpen)
	}
	return false
}

func groovySkipQuoted(lexer *gotreesitter.ExternalLexer) bool {
	quote := lexer.Lookahead()
	lexer.Advance(false)
	for lexer.Lookahead() != quote {
		if lexer.Lookahead() == 0 || lexer.Lookahead() == '\n' {
			return false
		}
		if lexer.Lookahead() == '\\' {
			lexer.Advance(false)
		}
		lexer.Advance(false)
	}
	return true
}

func (s GroovyExternalScanner) word(lexer *gotreesitter.ExternalLexer, valid []bool, newline bool) bool {
	lexer.MarkEnd()
	c := lexer.Lookahead()
	switch {
	case c == '\'' || c == '"' || c == '{' || c >= '0' && c <= '9':
		return valid[groovyCommandStart] && !newline && s.emit(lexer, groovyCommandStart)
	case !groovyWordRune(c):
		return false
	}
	word := groovyReadWord(lexer)
	switch {
	case word == "record" && valid[groovyRecordKeyword]:
		lexer.MarkEnd()
		groovySkipBlanks(lexer)
		if groovyReadWord(lexer) == "" {
			return false
		}
		groovySkipBlanks(lexer)
		return (lexer.Lookahead() == '(' || lexer.Lookahead() == '<') && s.emit(lexer, groovyRecordKeyword)
	case word == "var" && valid[groovyVarKeyword]:
		lexer.MarkEnd()
		groovySkipBlanks(lexer)
		return groovyWordRune(lexer.Lookahead()) && s.emit(lexer, groovyVarKeyword)
	case !valid[groovyCommandStart] || newline || groovyCommandStop(word):
		return false
	}
	groovySkipBlanks(lexer)
	switch lexer.Lookahead() {
	case '=':
		lexer.Advance(false)
		if c := lexer.Lookahead(); c != '=' && c != '~' {
			return false
		}
	case '(':
		if groovyDeclarationParameters(lexer) {
			return false
		}
	}
	return !groovyClosureParameters(lexer) && s.emit(lexer, groovyCommandStart)
}

func groovyCommandStop(word string) bool {
	switch word {
	case "in", "as", "instanceof", "else", "catch", "finally", "extends", "implements", "throws", "permits", "while":
		return true
	}
	return false
}

func groovyClosureParameters(lexer *gotreesitter.ExternalLexer) bool {
	depth := 0
	for {
		switch lexer.Lookahead() {
		case 0, '\n', '\r', '\'', '"':
			return false
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return false
			}
			depth--
		case '-':
			lexer.Advance(false)
			if depth == 0 && lexer.Lookahead() == '>' {
				return true
			}
			continue
		}
		lexer.Advance(false)
	}
}

func groovyDeclarationParameters(lexer *gotreesitter.ExternalLexer) bool {
	depth := 0
	for {
		c := lexer.Lookahead()
		switch {
		case c == 0:
			return false
		case depth == 1 && (c == '\'' || c == '"' || c == ':' || c >= '0' && c <= '9'):
			return false
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				lexer.Advance(false)
				groovySkipSpace(lexer)
				return lexer.Lookahead() == '{' || lexer.Lookahead() == 't'
			}
		}
		lexer.Advance(false)
	}
}

func groovyReadWord(lexer *gotreesitter.ExternalLexer) string {
	var word strings.Builder
	for groovyWordRune(lexer.Lookahead()) {
		word.WriteRune(lexer.Lookahead())
		lexer.Advance(false)
	}
	return word.String()
}

func groovySkipBlanks(lexer *gotreesitter.ExternalLexer) {
	for lexer.Lookahead() == ' ' || lexer.Lookahead() == '\t' {
		lexer.Advance(false)
	}
}

func groovySkipSpace(lexer *gotreesitter.ExternalLexer) {
	for groovySpace(lexer.Lookahead()) {
		lexer.Advance(false)
	}
}

func groovyWordRune(c rune) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c > 0x7f
}

func groovySpace(c rune) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (s GroovyExternalScanner) emit(lexer *gotreesitter.ExternalLexer, kind int) bool {
	lexer.SetResultSymbol(s.symbols[kind])
	return true
}
