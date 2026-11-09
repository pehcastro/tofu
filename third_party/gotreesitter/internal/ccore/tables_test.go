package ccore_test

import (
	"slices"
	"testing"

	"github.com/odvcencio/gotreesitter"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
	"github.com/odvcencio/gotreesitter/internal/ccore"
)

func groupScan(lang *gotreesitter.Language, state, symbol uint16) uint16 {
	if uint32(state) < lang.LargeStateCount {
		return lang.ParseTable[state][symbol]
	}
	data := lang.SmallParseTable[lang.SmallParseTableMap[uint32(state)-lang.LargeStateCount]:]
	i := 1
	for range int(data[0]) {
		value, count := data[i], int(data[i+1])
		if slices.Contains(data[i+2:i+2+count], symbol) {
			return value
		}
		i += 2 + count
	}
	return 0
}

func TestRankLookupEqualsGroupScan(t *testing.T) {
	for _, l := range []struct {
		name string
		load func() *gotreesitter.Language
	}{
		{"go", golang.LanguageC}, {"python", python.LanguageC}, {"rust", rust.LanguageC},
		{"typescript", typescript.LanguageC}, {"tsx", tsx.LanguageC}, {"javascript", javascript.LanguageC},
	} {
		lang := l.load()
		tables := &ccore.Tables{
			SymbolCount:     lang.SymbolCount,
			LargeStateCount: lang.LargeStateCount,
			ParseTable:      slices.Concat(lang.ParseTable...),
			Small:           ccore.NewSmallTable(lang.SymbolCount, lang.SmallParseTable, lang.SmallParseTableMap),
		}
		states := uint32(len(lang.SmallParseTableMap)) + lang.LargeStateCount
		checked, absent, wrong := 0, 0, 0
		for state := range states {
			for symbol := range lang.SymbolCount {
				got, want := tables.Lookup(uint16(state), uint16(symbol)), groupScan(lang, uint16(state), uint16(symbol))
				checked++
				if want == 0 && state >= lang.LargeStateCount {
					absent++
				}
				if got != want {
					wrong++
					if wrong <= 3 {
						t.Errorf("%s: state %d symbol %d: rank lookup %d, group scan %d", l.name, state, symbol, got, want)
					}
				}
			}
		}
		if wrong > 0 {
			t.Errorf("%s: %d of %d lookups differ", l.name, wrong, checked)
		}
		t.Logf("%s: %d states (%d large) x %d symbols, %d lookups, %d absent in small states, %d differ, index %d bytes", l.name, states, lang.LargeStateCount, lang.SymbolCount, checked, absent, wrong, tables.Small.Bytes())
	}
}
