package gotreesitter

import (
	"os"
	"sync"

	"github.com/odvcencio/gotreesitter/internal/ccore"
)

const CCoreSwitchEnv = "GOTREESITTER_CCORE"

type ccoreBinding struct {
	target     *Language
	source     *Language
	tables     *ccore.Tables
	symbols    []Symbol
	mapped     []bool
	fields     []FieldID
	supertypes []uint32
}

type ccoreEntry struct {
	load    func() *Language
	once    sync.Once
	binding *ccoreBinding
}

var ccoreRegistry sync.Map

func RegisterCCoreLanguage(name string, load func() *Language) {
	ccoreRegistry.Store(name, &ccoreEntry{load: load})
}

func (p *Parser) ccoreBinding() *ccoreBinding {
	if p.language == nil || len(p.included) > 0 || p.recoveryInitialOnly || p.logger != nil {
		return nil
	}
	value, ok := ccoreRegistry.Load(p.language.Name)
	if !ok {
		return nil
	}
	entry := value.(*ccoreEntry)
	entry.once.Do(func() { entry.binding = bindCCore(entry.load(), p.language) })
	if entry.binding == nil || entry.binding.target != p.language {
		return nil
	}
	return entry.binding
}

func ccoreSwitchedOff() bool { return os.Getenv(CCoreSwitchEnv) == "0" }

func (p *Parser) parseCCore(source []byte) (*Tree, bool) {
	if ccoreSwitchedOff() {
		return nil, false
	}
	return p.ParseNewCore(source)
}

func builtByNewCore(t *Tree) bool {
	return t != nil && t.incrementalReuseDisabled && t.incrementalReuseUnsupportedClause == newCoreIncrementalReuseClause
}

func (p *Parser) incrementalParsesFresh(oldTree *Tree) bool {
	return builtByNewCore(oldTree) || (!ccoreSwitchedOff() && p.ccoreBinding() != nil)
}

func (p *Parser) ParseNewCore(source []byte) (*Tree, bool) {
	b := p.ccoreBinding()
	if b == nil {
		return nil, false
	}
	var stop func() bool
	if p.needsParseBudget() {
		stop = func() bool { return p.activeParseStopReason() != ParseStopNone }
	}
	result, status := ccore.Parse(b.tables, source, stop)
	if status != ccore.Accepted {
		return nil, false
	}
	return b.materialize(&result, source)
}

func bindCCore(source, target *Language) *ccoreBinding {
	if source == nil || target == nil || source.ExternalTokenCount != 0 {
		return nil
	}
	b := &ccoreBinding{target: target, source: source, tables: ccoreTables(source)}
	b.symbols = make([]Symbol, len(source.SymbolNames))
	b.mapped = make([]bool, len(source.SymbolNames))
	seen := map[symbolNameNamedKey]int{}
	for sym, meta := range source.SymbolMetadata {
		if !meta.Visible {
			continue
		}
		key := symbolNameNamedKey{name: source.SymbolNames[sym], named: meta.Named}
		matches := ccoreVisibleSymbols(target, key)
		if len(matches) == 0 {
			continue
		}
		pick := len(matches) - 1
		if len(ccoreVisibleSymbols(source, key)) == len(matches) {
			pick = seen[key]
		}
		seen[key]++
		b.symbols[sym], b.mapped[sym] = matches[pick], true
	}
	b.fields = make([]FieldID, len(source.FieldNames))
	for id := 1; id < len(source.FieldNames); id++ {
		if fid, ok := target.FieldByName(source.FieldNames[id]); ok {
			b.fields[id] = fid
		}
	}
	b.supertypes = make([]uint32, len(source.SymbolNames))
	for sym, name := range source.SymbolNames {
		if source.supertypeBit(Symbol(sym)) == 0 {
			continue
		}
		for t, targetName := range target.SymbolNames {
			if bit := target.supertypeBit(Symbol(t)); bit != 0 && targetName == name {
				b.supertypes[sym] = bit
			}
		}
		if b.supertypes[sym] == 0 {
			return nil
		}
	}
	return b
}

func ccoreVisibleSymbols(lang *Language, key symbolNameNamedKey) []Symbol {
	var out []Symbol
	for sym, meta := range lang.SymbolMetadata {
		if meta.Visible && meta.Named == key.named && lang.SymbolNames[sym] == key.name {
			out = append(out, Symbol(sym))
		}
	}
	return out
}

func ccoreTables(lang *Language) *ccore.Tables {
	t := &ccore.Tables{
		SymbolCount:        lang.SymbolCount,
		TokenCount:         lang.TokenCount,
		LargeStateCount:    lang.LargeStateCount,
		SmallParseTable:    lang.SmallParseTable,
		SmallParseTableMap: lang.SmallParseTableMap,
		Main:               ccoreLexer(lang.LexStates),
		Keywords:           ccoreLexer(lang.KeywordLexStates),
		KeywordCapture:     uint16(lang.KeywordCaptureToken),
		ReservedSetSize:    uint32(lang.MaxReservedWordSetSize),
	}
	for _, row := range lang.ParseTable {
		t.ParseTable = append(t.ParseTable, row...)
	}
	for _, e := range lang.ParseActions {
		actions := make([]ccore.Action, len(e.Actions))
		for i, a := range e.Actions {
			actions[i] = ccore.Action{
				Type:         ccore.ActionType(a.Type),
				Extra:        a.Extra,
				Repetition:   a.Repetition,
				ChildCount:   a.ChildCount,
				Symbol:       uint16(a.Symbol),
				State:        uint16(a.State),
				ProductionID: a.ProductionID,
				DynPrec:      a.DynamicPrecedence,
			}
		}
		t.Actions = append(t.Actions, ccore.ActionEntry{Reusable: e.Reusable, Actions: actions})
	}
	for _, m := range lang.LexModes {
		t.LexModes = append(t.LexModes, ccore.LexMode{LexState: m.LexState, ReservedSet: m.ReservedWordSetID})
	}
	for _, w := range lang.ReservedWords {
		t.ReservedWords = append(t.ReservedWords, uint16(w))
	}
	return t
}

func ccoreLexer(states []LexState) ccore.Lexer {
	var l ccore.Lexer
	for _, s := range states {
		start := uint32(len(l.Transitions))
		for _, tr := range s.Transitions {
			l.Transitions = append(l.Transitions, ccore.LexTransition{Lo: tr.Lo, Hi: tr.Hi, Next: tr.NextState, Skip: tr.Skip})
		}
		l.States = append(l.States, ccore.LexState{
			Accepts:     s.AcceptToken != 0 || s.AcceptEOF,
			AcceptToken: uint16(s.AcceptToken),
			EOF:         int32(s.EOF),
			TransStart:  start,
			TransEnd:    uint32(len(l.Transitions)),
		})
	}
	return l
}
