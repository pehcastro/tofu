//go:build !grammar_subset || grammar_subset_powershell

package grammarruntime

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const (
	powershellTokStatementTerminator = iota
	powershellTokBareDashes
	powershellTokenCount
)

var powershellExternals = []string{"_statement_terminator", "_bare_dashes"}

type PowershellExternalScanner struct {
	symbols         [powershellTokenCount]gotreesitter.Symbol
	externalToToken []int
}

func (s PowershellExternalScanner) ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner {
	s.externalToToken = bindExternalScannerSymbolNames(lang, powershellExternals, func(tokenIdx int, sym gotreesitter.Symbol) {
		s.symbols[tokenIdx] = sym
	})
	return s
}

func (PowershellExternalScanner) Create() any                           { return nil }
func (PowershellExternalScanner) Destroy(payload any)                   {}
func (PowershellExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (PowershellExternalScanner) Deserialize(payload any, buf []byte)   {}
func (PowershellExternalScanner) SupportsIncrementalReuse() bool        { return true }
func (PowershellExternalScanner) ExternalScannerIsStateless() bool      { return true }
func (PowershellExternalScanner) PreservesStateOnScanFailure() bool     { return true }

func (s PowershellExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	var valid [powershellTokenCount]bool
	for externalIdx, ok := range validSymbols {
		if ok && externalIdx < len(s.externalToToken) && s.externalToToken[externalIdx] >= 0 {
			valid[s.externalToToken[externalIdx]] = true
		}
	}
	if valid[powershellTokBareDashes] && s.scanBareDashes(lexer) {
		return true
	}
	return valid[powershellTokStatementTerminator] && s.scanStatementTerminator(lexer)
}

func (s PowershellExternalScanner) scanStatementTerminator(lexer *gotreesitter.ExternalLexer) bool {
	lexer.SetResultSymbol(s.symbols[powershellTokStatementTerminator])
	lexer.MarkEnd()
	for {
		switch ch := lexer.Lookahead(); {
		case ch == 0 || ch == '}' || ch == ';' || ch == ')':
			return true
		case ch == '\n':
			return !powershellContinuesWithElse(lexer)
		case !unicode.IsSpace(ch):
			return false
		}
		lexer.Advance(true)
	}
}

func powershellContinuesWithElse(lexer *gotreesitter.ExternalLexer) bool {
	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}
	for _, want := range "else" {
		if unicode.ToLower(lexer.Lookahead()) != want {
			return false
		}
		lexer.Advance(true)
	}
	if unicode.ToLower(lexer.Lookahead()) == 'i' {
		lexer.Advance(true)
		if unicode.ToLower(lexer.Lookahead()) != 'f' {
			return false
		}
		lexer.Advance(true)
	}
	ch := lexer.Lookahead()
	return !(unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-')
}

func (s PowershellExternalScanner) scanBareDashes(lexer *gotreesitter.ExternalLexer) bool {
	for lexer.Lookahead() == ' ' || lexer.Lookahead() == '\t' {
		lexer.Advance(true)
	}
	switch lexer.Lookahead() {
	case '+':
		lexer.Advance(false)
	case '-':
		lexer.Advance(false)
		if lexer.Lookahead() == '-' {
			lexer.Advance(false)
		}
	default:
		return false
	}
	lexer.MarkEnd()
	switch lexer.Lookahead() {
	case 0, ' ', '\t', '\r', '\n', ';', '|', '&', ')', '}':
		lexer.SetResultSymbol(s.symbols[powershellTokBareDashes])
		return true
	}
	return false
}
