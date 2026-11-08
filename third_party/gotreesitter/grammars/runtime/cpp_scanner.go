//go:build !grammar_subset || grammar_subset_cpp

package grammarruntime

import (
	"unicode/utf8"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const (
	cppTokRawStringDelimiter = iota
	cppTokRawStringContent
	cppTokCfam
	cppTokenCount = cppTokCfam + cfamTokenCount
)

var cppExternals = []string{"raw_string_delimiter", "raw_string_content", "preproc_conditional", "preproc_disabled", "_directive_end", "macro_line", "macro_list_line", "macro_field_line"}

type CppExternalScanner struct {
	symbols         [cppTokenCount]gotreesitter.Symbol
	externalToToken []int
}

func (s CppExternalScanner) ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner {
	s.externalToToken = bindExternalScannerSymbolNames(lang, cppExternals, func(tokenIdx int, sym gotreesitter.Symbol) {
		s.symbols[tokenIdx] = sym
	})
	return s
}

func (CppExternalScanner) Create() any         { return rawStringCreate() }
func (CppExternalScanner) Destroy(payload any) {}

func (CppExternalScanner) Serialize(payload any, buf []byte) int {
	size := 1
	for _, r := range payload.(*rawStringState).delimiter {
		size += utf8.RuneLen(r)
	}
	if len(buf) < size {
		return 0
	}
	buf[0] = 1
	n := 1
	for _, r := range payload.(*rawStringState).delimiter {
		n += utf8.EncodeRune(buf[n:], r)
	}
	return n
}

func (CppExternalScanner) Deserialize(payload any, buf []byte) {
	if len(buf) > 0 && buf[0] == 1 {
		rawStringDeserialize(payload, buf[1:])
	} else {
		rawStringDeserialize(payload, nil)
	}
}

func (CppExternalScanner) SupportsCheckpointReadDependencies() bool    { return true }
func (CppExternalScanner) SupportsIncrementalReuse() bool              { return true }
func (CppExternalScanner) UsesExternalScannerCheckpoints() bool        { return true }
func (CppExternalScanner) SupportsIncrementalReuseFromErrorTree() bool { return false }

func (s CppExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	var valid [cppTokenCount]bool
	for externalIdx, ok := range validSymbols {
		if ok && externalIdx < len(s.externalToToken) && s.externalToToken[externalIdx] >= 0 {
			valid[s.externalToToken[externalIdx]] = true
		}
	}
	if valid[cppTokRawStringDelimiter] == valid[cppTokRawStringContent] {
		var cfamValid [cfamTokenCount]bool
		var cfamSymbols [cfamTokenCount]gotreesitter.Symbol
		copy(cfamValid[:], valid[cppTokCfam:])
		copy(cfamSymbols[:], s.symbols[cppTokCfam:])
		return scanCfam(lexer, cfamValid, preprocNever{"0"}, cfamSymbols)
	}
	return rawStringScan(payload, lexer, valid[:],
		cppTokRawStringDelimiter, cppTokRawStringContent,
		s.symbols[cppTokRawStringDelimiter], s.symbols[cppTokRawStringContent])
}
