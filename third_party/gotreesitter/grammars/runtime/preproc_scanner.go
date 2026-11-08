package grammarruntime

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

type preprocKind int

const (
	preprocIf preprocKind = iota
	preprocElse
	preprocEndif
	preprocOther
)

type preprocScan int

const (
	preprocNotHash preprocScan = iota
	preprocMatched
	preprocFailed
)

const (
	preprocTokConditional = iota
	preprocTokDisabled
	preprocTokDirectiveEnd
	preprocTokenCount
)

const (
	cfamTokMacroLine = preprocTokenCount + iota
	cfamTokMacroListLine
	cfamTokMacroFieldLine
	cfamTokenCount
)

type macroPlace int

const (
	macroInStatement macroPlace = iota
	macroInList
	macroInField
)

var cfamExternals = []string{"preproc_conditional", "preproc_disabled", "_directive_end", "macro_line", "macro_list_line", "macro_field_line"}

type preprocNever [2]string

type PreprocExternalScanner struct {
	never           preprocNever
	symbols         [cfamTokenCount]gotreesitter.Symbol
	externalToToken []int
}

func (s PreprocExternalScanner) ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner {
	s.externalToToken = bindExternalScannerSymbolNames(lang, cfamExternals, func(tokenIdx int, sym gotreesitter.Symbol) {
		s.symbols[tokenIdx] = sym
	})
	return s
}

func (PreprocExternalScanner) Create() any                           { return nil }
func (PreprocExternalScanner) Destroy(payload any)                   {}
func (PreprocExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (PreprocExternalScanner) Deserialize(payload any, buf []byte)   {}

func (s PreprocExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	var valid [cfamTokenCount]bool
	for externalIdx, ok := range validSymbols {
		if ok && externalIdx < len(s.externalToToken) && s.externalToToken[externalIdx] >= 0 {
			valid[s.externalToToken[externalIdx]] = true
		}
	}
	return scanCfam(lexer, valid, s.never, s.symbols)
}

func scanCfam(lexer *gotreesitter.ExternalLexer, valid [cfamTokenCount]bool, never preprocNever, symbols [cfamTokenCount]gotreesitter.Symbol) bool {
	if valid[preprocTokDirectiveEnd] && scanDirectiveEnd(lexer, symbols[preprocTokDirectiveEnd]) {
		return true
	}
	if valid[preprocTokConditional] {
		tokens := preprocTokens{never: never, conditional: symbols[preprocTokConditional], disabled: symbols[preprocTokDisabled]}
		if scanned := tokens.scan(lexer); scanned != preprocNotHash {
			return scanned == preprocMatched
		}
	}
	if valid[cfamTokMacroLine] {
		place := macroInStatement
		if valid[cfamTokMacroFieldLine] {
			place = macroInField
		}
		return scanMacroLine(lexer, symbols[cfamTokMacroLine], symbols[cfamTokMacroFieldLine], place)
	}
	return valid[cfamTokMacroListLine] && scanMacroLine(lexer, symbols[cfamTokMacroListLine], 0, macroInList)
}

type preprocTokens struct {
	never       preprocNever
	conditional gotreesitter.Symbol
	disabled    gotreesitter.Symbol
}

func preprocSpaces(lexer *gotreesitter.ExternalLexer) {
	for lexer.Lookahead() == ' ' || lexer.Lookahead() == '\t' {
		lexer.Advance(false)
	}
}

func lineSpaces(lexer *gotreesitter.ExternalLexer) {
	for c := lexer.Lookahead(); c == ' ' || c == '\t' || c == '\r'; c = lexer.Lookahead() {
		lexer.Advance(false)
	}
}

func preprocDirective(lexer *gotreesitter.ExternalLexer) (string, preprocKind) {
	lexer.Advance(false)
	preprocSpaces(lexer)
	directive, _, _ := scanIdentifier(lexer)
	switch directive {
	case "if", "ifdef", "ifndef":
		return directive, preprocIf
	case "else", "elif", "elifdef", "elifndef":
		return directive, preprocElse
	case "endif":
		return directive, preprocEndif
	}
	return directive, preprocOther
}

func preprocRestOfLine(lexer *gotreesitter.ExternalLexer) {
	for c := lexer.Lookahead(); c != 0 && c != '\n'; c = lexer.Lookahead() {
		if c == '\\' {
			lexer.Advance(false)
			if lexer.Lookahead() == '\r' {
				lexer.Advance(false)
			}
		}
		lexer.Advance(false)
	}
}

func preprocLine(lexer *gotreesitter.ExternalLexer) {
	preprocRestOfLine(lexer)
	if lexer.Lookahead() == '\n' {
		lexer.Advance(false)
	}
}

func preprocSkipDisabled(lexer *gotreesitter.ExternalLexer, stopAtElse bool) {
	depth := 0
	for lexer.Lookahead() != 0 {
		preprocSpaces(lexer)
		if lexer.Lookahead() == '#' {
			switch _, kind := preprocDirective(lexer); kind {
			case preprocIf:
				depth++
			case preprocEndif:
				if depth == 0 {
					return
				}
				depth--
			case preprocElse:
				if depth == 0 && stopAtElse {
					preprocLine(lexer)
					lexer.MarkEnd()
					return
				}
			}
		}
		preprocLine(lexer)
		lexer.MarkEnd()
	}
}

func (never preprocNever) taken(lexer *gotreesitter.ExternalLexer, directive string) bool {
	if directive == "ifndef" {
		return true
	}
	preprocSpaces(lexer)
	word, _, _ := scanIdentifier(lexer)
	if directive == "if" && word == "defined" {
		preprocSpaces(lexer)
		parens := lexer.Lookahead() == '('
		if parens {
			lexer.Advance(false)
		}
		preprocSpaces(lexer)
		word, _, _ = scanIdentifier(lexer)
		preprocSpaces(lexer)
		if parens && lexer.Lookahead() != ')' {
			return true
		}
		if parens {
			lexer.Advance(false)
		}
	}
	preprocSpaces(lexer)
	if c := lexer.Lookahead(); c != '\n' && c != '\r' && c != '/' && c != 0 {
		return true
	}
	for _, w := range never {
		if w != "" && w == word && (w != "0" || directive == "if") {
			return false
		}
	}
	return true
}

func (t preprocTokens) scan(lexer *gotreesitter.ExternalLexer) preprocScan {
	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}
	if lexer.Lookahead() != '#' {
		return preprocNotHash
	}
	directive, kind := preprocDirective(lexer)
	switch kind {
	case preprocOther:
		return preprocFailed
	case preprocElse:
		preprocLine(lexer)
		lexer.MarkEnd()
		preprocSkipDisabled(lexer, false)
		lexer.SetResultSymbol(t.disabled)
		return preprocMatched
	}
	off := kind == preprocIf && !t.never.taken(lexer, directive)
	preprocRestOfLine(lexer)
	lexer.MarkEnd()
	if !off {
		lexer.SetResultSymbol(t.conditional)
		return preprocMatched
	}
	if lexer.Lookahead() == '\n' {
		lexer.Advance(false)
	}
	lexer.MarkEnd()
	preprocSkipDisabled(lexer, true)
	lexer.SetResultSymbol(t.disabled)
	return preprocMatched
}

func scanMacroArgs(lexer *gotreesitter.ExternalLexer) (ok, holdsStatement bool) {
	depth := 0
	for {
		c := lexer.Lookahead()
		switch c {
		case 0:
			return false, holdsStatement
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			holdsStatement = true
		case '"', '\'':
			lexer.Advance(false)
			for lexer.Lookahead() != c {
				if lexer.Lookahead() == 0 || lexer.Lookahead() == '\n' {
					return false, holdsStatement
				}
				if lexer.Lookahead() == '\\' {
					lexer.Advance(false)
				}
				lexer.Advance(false)
			}
		}
		lexer.Advance(false)
		if depth == 0 {
			return true, holdsStatement
		}
	}
}

func isCKeywordWithParens(name string) bool {
	switch name {
	case "if", "while", "for", "switch", "return", "sizeof", "else", "do", "case", "goto",
		"typeof", "__typeof__", "__typeof", "_Generic", "__attribute__", "__attribute", "__declspec",
		"asm", "__asm__", "__asm", "_Alignas", "alignas", "_Alignof", "alignof", "static_assert",
		"_Static_assert", "defined", "__extension__":
		return true
	}
	return false
}

func scanIdentifier(lexer *gotreesitter.ExternalLexer) (name string, upper, lower bool) {
	var buf [32]byte
	n := 0
	for c := lexer.Lookahead(); c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c); c = lexer.Lookahead() {
		upper = upper || unicode.IsUpper(c)
		lower = lower || unicode.IsLower(c)
		if n < len(buf) {
			buf[n] = byte(c)
		}
		n++
		lexer.Advance(false)
	}
	if n > len(buf) {
		return "", upper, lower
	}
	return string(buf[:n]), upper, lower
}

func scanMacroLine(lexer *gotreesitter.ExternalLexer, sym, fieldSym gotreesitter.Symbol, place macroPlace) bool {
	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}
	if c := lexer.Lookahead(); c != '_' && !unicode.IsLetter(c) {
		return false
	}
	name, upper, lower := scanIdentifier(lexer)
	if isCKeywordWithParens(name) {
		return false
	}
	preprocSpaces(lexer)
	args := lexer.Lookahead() == '('
	if !args && (lower || !upper) {
		return false
	}
	holdsStatement := false
	if args {
		ok, statement := scanMacroArgs(lexer)
		if !ok {
			return false
		}
		holdsStatement = statement
	}
	preprocSpaces(lexer)
	if holdsStatement && lexer.Lookahead() == ';' {
		lexer.Advance(false)
	}
	if place == macroInField && args && upper && !lower && !holdsStatement && lexer.Lookahead() == ';' {
		lexer.Advance(false)
		sym = fieldSym
	}
	lineSpaces(lexer)
	inList := place == macroInList
	if c := lexer.Lookahead(); !inList && (c == '{' && !args || c == '}' && args) {
		lexer.MarkEnd()
		lexer.SetResultSymbol(sym)
		return true
	}
	comment := lexer.Lookahead() == '/'
	if comment {
		lexer.MarkEnd()
		lexer.Advance(false)
		if lexer.Lookahead() != '/' {
			return false
		}
		preprocRestOfLine(lexer)
	}
	switch lexer.Lookahead() {
	case '\n':
		lexer.Advance(false)
	case 0:
	default:
		return false
	}
	if !comment {
		lexer.MarkEnd()
	}
	lineSpaces(lexer)
	c := skipLineComments(lexer)
	if c != 0 && !startsMacroFollower(c) {
		return false
	}
	if inList && listEndsAfterDirectives(lexer) {
		return false
	}
	if !args && !inList && (c == '_' || unicode.IsLetter(c)) {
		if next, _, nextLower := scanIdentifier(lexer); nextLower && !opensDeclaration(next) {
			return false
		}
	}
	lexer.SetResultSymbol(sym)
	return true
}

func skipLineComments(lexer *gotreesitter.ExternalLexer) rune {
	for lexer.Lookahead() == '/' {
		lexer.Advance(false)
		if lexer.Lookahead() != '/' {
			return '/'
		}
		preprocLine(lexer)
		for unicode.IsSpace(lexer.Lookahead()) {
			lexer.Advance(false)
		}
	}
	return lexer.Lookahead()
}

func startsMacroFollower(c rune) bool {
	switch c {
	case '_', '#', '}', '/', '[', '~', '\n':
		return true
	}
	return unicode.IsLetter(c)
}

func opensDeclaration(word string) bool {
	switch word {
	case "template", "namespace", "class", "struct", "union", "enum", "typedef", "using", "extern", "static",
		"inline", "constexpr", "void", "public", "private", "protected", "friend", "virtual", "explicit":
		return true
	}
	return false
}

func listEndsAfterDirectives(lexer *gotreesitter.ExternalLexer) bool {
	for c := lexer.Lookahead(); c == '#' || unicode.IsSpace(c); c = lexer.Lookahead() {
		if c == '#' {
			preprocLine(lexer)
			continue
		}
		lexer.Advance(false)
	}
	return lexer.Lookahead() == '}' || lexer.Lookahead() == ','
}

func scanDirectiveEnd(lexer *gotreesitter.ExternalLexer, sym gotreesitter.Symbol) bool {
	for c := lexer.Lookahead(); c == ' ' || c == '\t' || c == '\r'; c = lexer.Lookahead() {
		lexer.Advance(true)
	}
	switch lexer.Lookahead() {
	case '\n':
		lexer.Advance(false)
	case 0:
	default:
		return false
	}
	lexer.MarkEnd()
	lexer.SetResultSymbol(sym)
	return true
}
