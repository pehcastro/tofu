package gotreesitter

import (
	"bytes"
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
	works      sync.Pool
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
	work := b.works.Get().(*ccore.Work)
	defer b.works.Put(work)
	result, ok := p.parseCCoreInto(b, work, source)
	if !ok {
		return nil, false
	}
	return b.materialize(result, source)
}

func (p *Parser) ParseFlat(source []byte, into []FlatNode) ([]FlatNode, *Tree, error) {
	if b := p.ccoreBinding(); b != nil && !ccoreSwitchedOff() {
		defer p.enterParseBudget()()
		work := b.works.Get().(*ccore.Work)
		result, ok := p.parseCCoreInto(b, work, source)
		if ok {
			into, ok = b.flatten(result, into)
		}
		b.works.Put(work)
		if ok {
			return into, nil, nil
		}
	}
	tree, err := p.Parse(source)
	return into, tree, err
}

func (p *Parser) parseCCoreInto(b *ccoreBinding, work *ccore.Work, source []byte) (*ccore.Result, bool) {
	var stop func() bool
	if p.needsParseBudget() {
		stop = func() bool { return p.activeParseStopReason() != ParseStopNone }
	}
	var scanner ccore.Scanner
	if s := b.source.ExternalScanner; s != nil {
		cs := &ccoreScanner{scanner: s, payload: s.Create(), source: source}
		defer s.Destroy(cs.payload)
		scanner = cs
	}
	result, status := work.Parse(b.tables, source, scanner, stop)
	return result, status == ccore.Accepted
}

type ccoreScanner struct {
	scanner ExternalScanner
	payload any
	lexer   ExternalLexer
	source  []byte
	rowAt   uint32
	row     uint32
	line    uint32
}

func (s *ccoreScanner) point(at uint32) Point {
	if at >= s.rowAt {
		gap := s.source[s.rowAt:at]
		if crossed := bytes.Count(gap, []byte{'\n'}); crossed > 0 {
			s.row += uint32(crossed)
			s.line = s.rowAt + uint32(bytes.LastIndexByte(gap, '\n')+1)
		}
	} else {
		s.row -= uint32(bytes.Count(s.source[at:s.rowAt], []byte{'\n'}))
		if at < s.line {
			s.line = uint32(bytes.LastIndexByte(s.source[:at], '\n') + 1)
		}
	}
	s.rowAt = at
	return Point{Row: s.row, Column: at - s.line}
}

func (s *ccoreScanner) Scan(at uint32, valid []bool, state []byte) (uint16, uint32, uint32, bool) {
	s.scanner.Deserialize(s.payload, state)
	l := &s.lexer
	pt := s.point(at)
	l.reset(s.source, int(at), pt.Row, pt.Column)
	if !s.scanner.Scan(s.payload, l, valid) || !l.hasResult {
		return 0, 0, 0, false
	}
	end := uint32(l.pos)
	if l.endMarked {
		end = uint32(l.endPos)
	}
	return uint16(l.resultSymbol), min(uint32(l.startPos), end), end, true
}

func (s *ccoreScanner) Serialize(buf []byte) int { return s.scanner.Serialize(s.payload, buf) }

func bindCCore(source, target *Language) *ccoreBinding {
	if source == nil || target == nil || source.ExternalTokenCount != 0 && source.ExternalScanner == nil {
		return nil
	}
	b := &ccoreBinding{target: target, source: source, tables: ccoreTables(source)}
	b.works.New = func() any { return &ccore.Work{} }
	b.symbols = make([]Symbol, len(source.SymbolNames))
	b.mapped = make([]bool, len(source.SymbolNames))
	seen := map[symbolNameNamedKey]int{}
	targetVisible, sourceVisible := ccoreVisibleSymbols(target), ccoreVisibleSymbols(source)
	for sym, meta := range source.SymbolMetadata {
		if !meta.Visible {
			continue
		}
		key := symbolNameNamedKey{name: source.SymbolNames[sym], named: meta.Named}
		matches := targetVisible[key]
		if len(matches) == 0 {
			if hidden, ok := target.symbolByNameAndNamed(key.name, key.named); ok {
				b.symbols[sym], b.mapped[sym] = hidden, true
			}
			continue
		}
		pick := len(matches) - 1
		if len(sourceVisible[key]) == len(matches) {
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

func ccoreVisibleSymbols(lang *Language) map[symbolNameNamedKey][]Symbol {
	out := make(map[symbolNameNamedKey][]Symbol, len(lang.SymbolMetadata))
	for sym, meta := range lang.SymbolMetadata {
		if meta.Visible {
			key := symbolNameNamedKey{name: lang.SymbolNames[sym], named: meta.Named}
			out[key] = append(out[key], Symbol(sym))
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
		ExternalStates:     lang.ExternalLexStates,
	}
	t.ParseTable = make([]uint16, 0, len(lang.ParseTable)*int(lang.SymbolCount))
	for _, row := range lang.ParseTable {
		t.ParseTable = append(t.ParseTable, row...)
	}
	actionCount := 0
	for _, e := range lang.ParseActions {
		actionCount += len(e.Actions)
	}
	slab := make([]ccore.Action, 0, actionCount)
	t.Actions = make([]ccore.ActionEntry, len(lang.ParseActions))
	for i, e := range lang.ParseActions {
		first := len(slab)
		for _, a := range e.Actions {
			slab = append(slab, ccore.Action{
				Type:         ccore.ActionType(a.Type),
				Extra:        a.Extra,
				Repetition:   a.Repetition,
				ChildCount:   a.ChildCount,
				Symbol:       uint16(a.Symbol),
				State:        uint16(a.State),
				ProductionID: a.ProductionID,
				DynPrec:      a.DynamicPrecedence,
			})
		}
		t.Actions[i] = ccore.ActionEntry{Reusable: e.Reusable, Actions: slab[first:len(slab):len(slab)]}
	}
	t.LexModes = make([]ccore.LexMode, len(lang.LexModes))
	for i, m := range lang.LexModes {
		t.LexModes[i] = ccore.LexMode{LexState: m.LexState, External: m.ExternalLexState, ReservedSet: m.ReservedWordSetID}
	}
	t.ReservedWords = make([]uint16, len(lang.ReservedWords))
	for i, w := range lang.ReservedWords {
		t.ReservedWords[i] = uint16(w)
	}
	return t
}

func ccoreLexer(states []LexState) ccore.Lexer {
	transitions := 0
	for _, s := range states {
		transitions += len(s.Transitions)
	}
	l := ccore.Lexer{States: make([]ccore.LexState, len(states)), Transitions: make([]ccore.LexTransition, 0, transitions)}
	for i, s := range states {
		start := uint32(len(l.Transitions))
		for _, tr := range s.Transitions {
			l.Transitions = append(l.Transitions, ccore.LexTransition{Lo: tr.Lo, Hi: tr.Hi, Next: tr.NextState, Skip: tr.Skip})
		}
		l.States[i] = ccore.LexState{
			Accepts:     s.AcceptToken != 0 || s.AcceptEOF,
			AcceptToken: uint16(s.AcceptToken),
			EOF:         int32(s.EOF),
			TransStart:  start,
			TransEnd:    uint32(len(l.Transitions)),
		}
	}
	return l
}
