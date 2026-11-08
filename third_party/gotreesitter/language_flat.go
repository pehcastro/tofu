package gotreesitter

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"unsafe"
)

const (
	flatBlobMagic      = "GTSFLAT1"
	flatBlobHeaderSize = 16
	flatBlobMaxRaw     = 256 << 20
	flatBlobChunk      = 512 << 10
)

var errFlatBlobCorrupt = errors.New("corrupt flat grammar blob")

type flatScalars struct {
	LanguageVersion                          uint32
	SymbolCount                              uint32
	TokenCount                               uint32
	ExternalTokenCount                       uint32
	StateCount                               uint32
	LargeStateCount                          uint32
	FieldCount                               uint32
	ProductionIDCount                        uint32
	InitialState                             StateID
	KeywordCaptureToken                      Symbol
	LayoutFallbackLexState                   uint16
	MaxReservedWordSetSize                   uint16
	Metadata                                 LanguageMetadata
	GeneratedByGrammargen                    bool
	CRecoveryCostCompetitionCapable          bool
	CRecoveryCostCompetitionEnabledByDefault bool
	WantsForest                              bool
	HasLayoutFallbackLexState                bool
}

type flatSymbolFlags struct {
	Visible, Named, Supertype, GeneratedRepeatAux bool
}

type flatLexState struct {
	AcceptToken    Symbol
	AcceptPriority int16
	Skip           bool
	AcceptEOF      bool
	Default        int32
	EOF            int32
	First          uint32
	Count          uint32
}

type flatGoto struct {
	Key    uint64
	Target StateID
}

type flatWriter struct {
	pool []byte
	seen map[string]uint32
	body []byte
}

func flatPut[T any](w *flatWriter, s []T) {
	w.body = binary.LittleEndian.AppendUint64(w.body, uint64(len(s)))
	size := int(unsafe.Sizeof(*new(T)))
	at := len(w.body)
	w.body = append(w.body, make([]byte, (len(s)*size+7)&^7)...)
	for i := range s {
		flatPutFields(w.body[at+i*size:], reflect.ValueOf(&s[i]).Elem())
	}
}

func flatPutFields(dst []byte, v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			flatPutFields(dst[v.Type().Field(i).Offset:], v.Field(i))
		}
	case reflect.Array:
		for i := range v.Len() {
			flatPutFields(dst[uintptr(i)*v.Type().Elem().Size():], v.Index(i))
		}
	default:
		copy(dst, unsafe.Slice((*byte)(v.Addr().UnsafePointer()), v.Type().Size()))
	}
}

func flatPutRows[T any](w *flatWriter, rows [][]T) {
	lens := make([]uint32, len(rows))
	var flat []T
	for i, row := range rows {
		lens[i] = uint32(len(row))
		flat = append(flat, row...)
	}
	flatPut(w, lens)
	flatPut(w, flat)
}

func flatPutDetached[T, E any](w *flatWriter, items []T, detach func(*T) []E) {
	heads := slices.Clone(items)
	rows := make([][]E, len(heads))
	for i := range heads {
		rows[i] = detach(&heads[i])
	}
	flatPut(w, heads)
	flatPutRows(w, rows)
}

func (w *flatWriter) putStrings(ss []string) {
	refs := make([][2]uint32, len(ss))
	for i, s := range ss {
		at, ok := w.seen[s]
		if !ok {
			at = uint32(len(w.pool))
			w.pool = append(w.pool, s...)
			w.seen[s] = at
		}
		refs[i] = [2]uint32{at, uint32(len(s))}
	}
	flatPut(w, refs)
}

func (w *flatWriter) putLexStates(groups ...[]LexState) {
	var pool []LexTransition
	spans := map[string]uint32{}
	for _, states := range groups {
		records := make([]flatLexState, len(states))
		for i, s := range states {
			span := &flatWriter{}
			flatPut(span, s.Transitions)
			first, ok := spans[string(span.body)]
			if !ok {
				first = uint32(len(pool))
				pool = append(pool, s.Transitions...)
				spans[string(span.body)] = first
			}
			records[i] = flatLexState{AcceptToken: s.AcceptToken, AcceptPriority: s.AcceptPriority, Skip: s.Skip, AcceptEOF: s.AcceptEOF, Default: int32(s.Default), EOF: int32(s.EOF), First: first, Count: uint32(len(s.Transitions))}
		}
		flatPut(w, records)
	}
	flatPut(w, pool)
}

func EncodeLanguageBlob(lang *Language, compress func(raw []byte) []byte) []byte {
	w := &flatWriter{seen: map[string]uint32{}}
	flatPut(w, []flatScalars{{
		LanguageVersion: lang.LanguageVersion, SymbolCount: lang.SymbolCount, TokenCount: lang.TokenCount,
		ExternalTokenCount: lang.ExternalTokenCount, StateCount: lang.StateCount, LargeStateCount: lang.LargeStateCount,
		FieldCount: lang.FieldCount, ProductionIDCount: lang.ProductionIDCount, InitialState: lang.InitialState,
		KeywordCaptureToken: lang.KeywordCaptureToken, LayoutFallbackLexState: lang.LayoutFallbackLexState,
		MaxReservedWordSetSize: lang.MaxReservedWordSetSize, Metadata: lang.Metadata,
		GeneratedByGrammargen: lang.GeneratedByGrammargen, CRecoveryCostCompetitionCapable: lang.CRecoveryCostCompetitionCapable,
		CRecoveryCostCompetitionEnabledByDefault: lang.CRecoveryCostCompetitionEnabledByDefault, WantsForest: lang.WantsForest,
		HasLayoutFallbackLexState: lang.HasLayoutFallbackLexState,
	}})
	metaNames := make([]string, len(lang.SymbolMetadata))
	flags := make([]flatSymbolFlags, len(lang.SymbolMetadata))
	for i, m := range lang.SymbolMetadata {
		metaNames[i] = m.Name
		flags[i] = flatSymbolFlags{m.Visible, m.Named, m.Supertype, m.GeneratedRepeatAux}
	}
	w.putStrings([]string{lang.Name, cRecoveryTablesFailure(lang)})
	w.putStrings(lang.SymbolNames)
	w.putStrings(metaNames)
	flatPut(w, flags)
	w.putStrings(lang.FieldNames)
	flatPutRows(w, lang.ParseTable)
	flatPut(w, lang.SmallParseTable)
	flatPut(w, lang.SmallParseTableMap)
	reusable := make([]bool, len(lang.ParseActions))
	actions := make([][]ParseAction, len(lang.ParseActions))
	for i, e := range lang.ParseActions {
		reusable[i], actions[i] = e.Reusable, e.Actions
	}
	flatPut(w, reusable)
	flatPutRows(w, actions)
	gotos := make([]flatGoto, 0, len(lang.LargeStateGotos))
	for k, v := range lang.LargeStateGotos {
		gotos = append(gotos, flatGoto{k, v})
	}
	slices.SortFunc(gotos, func(a, b flatGoto) int { return cmp.Compare(a.Key, b.Key) })
	flatPut(w, gotos)
	flatPutDetached(w, lang.ReduceChainHints, func(h *ReduceChainHint) []StateID {
		states := h.TerminalStates
		h.TerminalStates = nil
		return states
	})
	flatPutDetached(w, lang.ConflictPolicies, func(p *ConflictPolicy) []Symbol {
		syms := p.ReduceSymbols
		p.ReduceSymbols = nil
		return syms
	})
	flatPut(w, lang.LexModes)
	w.putLexStates(lang.LexStates, lang.KeywordLexStates)
	flatPut(w, lang.FieldMapSlices)
	flatPut(w, lang.FieldMapEntries)
	flatPutRows(w, lang.AliasSequences)
	flatPutDetached(w, lang.ProductionSignatures, func(p *ProductionSignature) []Symbol {
		rhs := p.RHS
		p.RHS = nil
		return rhs
	})
	flatPut(w, lang.PrimaryStateIDs)
	flatPut(w, lang.ReservedWords)
	flatPut(w, lang.SupertypeSymbols)
	flatPut(w, lang.SupertypeMapSlices)
	flatPut(w, lang.SupertypeMapEntries)
	flatPut(w, lang.HiddenChoicePassthroughSymbols)
	flatPut(w, lang.ExternalSymbols)
	flatPut(w, lang.ImmediateTokens)
	flatPut(w, lang.ZeroWidthTokens)
	flatPutRows(w, lang.ExternalLexStates)
	flatPutRows(w, lang.NonTerminalAliasMap)
	out := &flatWriter{}
	flatPut(out, w.pool)
	raw := slices.Concat(out.body, w.body)
	blob := binary.LittleEndian.AppendUint64([]byte(flatBlobMagic), uint64(len(raw)))
	var body []byte
	for at := 0; at < len(raw); at += flatBlobChunk {
		body = append(body, compress(raw[at:min(at+flatBlobChunk, len(raw))])...)
		blob = binary.LittleEndian.AppendUint32(blob, uint32(len(body)))
	}
	return append(blob, body...)
}

type flatReader struct {
	buf  []byte
	pos  int
	pool string
	err  error
}

func flatGet[T any](r *flatReader) []T {
	if r.err != nil {
		return nil
	}
	if r.pos+8 > len(r.buf) {
		r.err = errFlatBlobCorrupt
		return nil
	}
	n := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	size := uint64(unsafe.Sizeof(*new(T)))
	if n > uint64(len(r.buf)-r.pos)/size {
		r.err = errFlatBlobCorrupt
		return nil
	}
	if n == 0 {
		return nil
	}
	s := unsafe.Slice((*T)(unsafe.Pointer(&r.buf[r.pos])), n)
	r.pos += int((n*size + 7) &^ 7)
	return s
}

func flatGetRows[T any](r *flatReader) [][]T {
	lens := flatGet[uint32](r)
	flat := flatGet[T](r)
	if len(lens) == 0 {
		return nil
	}
	rows := make([][]T, len(lens))
	at := 0
	for i, n := range lens {
		if int(n) > len(flat)-at {
			r.err = errFlatBlobCorrupt
			return nil
		}
		if n > 0 {
			rows[i] = flat[at : at+int(n) : at+int(n)]
		}
		at += int(n)
	}
	return rows
}

func flatGetAttached[T, E any](r *flatReader, attach func(*T, []E)) []T {
	heads := slices.Clone(flatGet[T](r))
	rows := flatGetRows[E](r)
	if len(rows) != len(heads) {
		r.err = errFlatBlobCorrupt
		return nil
	}
	for i := range heads {
		attach(&heads[i], rows[i])
	}
	return heads
}

func (r *flatReader) strings() []string {
	refs := flatGet[[2]uint32](r)
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		if uint64(ref[0])+uint64(ref[1]) > uint64(len(r.pool)) {
			r.err = errFlatBlobCorrupt
			return nil
		}
		out[i] = r.pool[ref[0] : ref[0]+ref[1]]
	}
	return out
}

func (r *flatReader) lexStates(records []flatLexState, pool []LexTransition) []LexState {
	if len(records) == 0 {
		return nil
	}
	states := make([]LexState, len(records))
	for i, s := range records {
		if uint64(s.First)+uint64(s.Count) > uint64(len(pool)) {
			r.err = errFlatBlobCorrupt
			return nil
		}
		states[i] = LexState{AcceptToken: s.AcceptToken, AcceptPriority: s.AcceptPriority, Skip: s.Skip, AcceptEOF: s.AcceptEOF, Default: int(s.Default), EOF: int(s.EOF)}
		if s.Count > 0 {
			states[i].Transitions = pool[s.First : s.First+s.Count : s.First+s.Count]
		}
	}
	return states
}

func LoadLanguage(data []byte) (*Language, error) {
	if len(data) < flatBlobHeaderSize || string(data[:8]) != flatBlobMagic {
		return nil, errors.New("decode language: not a flat grammar blob")
	}
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		return nil, errors.New("decode language: flat grammar blobs need a little-endian host")
	}
	size := binary.LittleEndian.Uint64(data[8:flatBlobHeaderSize])
	if size == 0 || size%8 != 0 || size > flatBlobMaxRaw {
		return nil, fmt.Errorf("decode language: %w", errFlatBlobCorrupt)
	}
	chunks := int((size + flatBlobChunk - 1) / flatBlobChunk)
	if len(data)-flatBlobHeaderSize < 4*chunks {
		return nil, fmt.Errorf("decode language: %w", errFlatBlobCorrupt)
	}
	words := make([]uint64, size/8)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), size)
	ends := data[flatBlobHeaderSize : flatBlobHeaderSize+4*chunks]
	if err := lz4DecodeChunks(raw, ends, data[flatBlobHeaderSize+4*chunks:]); err != nil {
		return nil, fmt.Errorf("decode language: %w", err)
	}
	lang, err := decodeFlatLanguage(raw)
	if err != nil {
		return nil, fmt.Errorf("decode language: %w", err)
	}
	lang.grammarBlobSHA256 = sha256.Sum256(data)
	lang.grammarBlobSHA256Valid = true
	return lang, nil
}

func decodeFlatLanguage(raw []byte) (*Language, error) {
	r := &flatReader{buf: raw}
	pool := flatGet[byte](r)
	r.pool = unsafe.String(unsafe.SliceData(pool), len(pool))
	scalars := flatGet[flatScalars](r)
	if len(scalars) != 1 {
		return nil, errFlatBlobCorrupt
	}
	s := scalars[0]
	lang := &Language{
		LanguageVersion: s.LanguageVersion, SymbolCount: s.SymbolCount, TokenCount: s.TokenCount,
		ExternalTokenCount: s.ExternalTokenCount, StateCount: s.StateCount, LargeStateCount: s.LargeStateCount,
		FieldCount: s.FieldCount, ProductionIDCount: s.ProductionIDCount, InitialState: s.InitialState,
		KeywordCaptureToken: s.KeywordCaptureToken, LayoutFallbackLexState: s.LayoutFallbackLexState,
		MaxReservedWordSetSize: s.MaxReservedWordSetSize, Metadata: s.Metadata,
		GeneratedByGrammargen: s.GeneratedByGrammargen, CRecoveryCostCompetitionCapable: s.CRecoveryCostCompetitionCapable,
		CRecoveryCostCompetitionEnabledByDefault: s.CRecoveryCostCompetitionEnabledByDefault, WantsForest: s.WantsForest,
		HasLayoutFallbackLexState: s.HasLayoutFallbackLexState,
	}
	head := r.strings()
	if len(head) != 2 {
		return nil, errFlatBlobCorrupt
	}
	lang.Name, lang.cRecoveryTablesFailure, lang.cRecoveryTablesKnown = head[0], head[1], true
	lang.SymbolNames = r.strings()
	metaNames := r.strings()
	flags := flatGet[flatSymbolFlags](r)
	if len(flags) != len(metaNames) {
		return nil, errFlatBlobCorrupt
	}
	if len(flags) > 0 {
		lang.SymbolMetadata = make([]SymbolMetadata, len(flags))
		for i, f := range flags {
			lang.SymbolMetadata[i] = SymbolMetadata{Name: metaNames[i], Visible: f.Visible, Named: f.Named, Supertype: f.Supertype, GeneratedRepeatAux: f.GeneratedRepeatAux}
		}
	}
	lang.FieldNames = r.strings()
	lang.ParseTable = flatGetRows[uint16](r)
	lang.SmallParseTable = flatGet[uint16](r)
	lang.SmallParseTableMap = flatGet[uint32](r)
	reusable := flatGet[bool](r)
	actions := flatGetRows[ParseAction](r)
	if len(actions) != len(reusable) {
		return nil, errFlatBlobCorrupt
	}
	if len(actions) > 0 {
		lang.ParseActions = make([]ParseActionEntry, len(actions))
		for i := range actions {
			lang.ParseActions[i] = ParseActionEntry{Reusable: reusable[i], Actions: actions[i]}
		}
	}
	if gotos := flatGet[flatGoto](r); len(gotos) > 0 {
		lang.LargeStateGotos = make(map[uint64]StateID, len(gotos))
		for _, g := range gotos {
			lang.LargeStateGotos[g.Key] = g.Target
		}
	}
	lang.ReduceChainHints = flatGetAttached(r, func(h *ReduceChainHint, row []StateID) { h.TerminalStates = row })
	lang.ConflictPolicies = flatGetAttached(r, func(p *ConflictPolicy, row []Symbol) { p.ReduceSymbols = row })
	lang.LexModes = flatGet[LexMode](r)
	lexRecords := flatGet[flatLexState](r)
	keywordRecords := flatGet[flatLexState](r)
	transitions := flatGet[LexTransition](r)
	lang.LexStates = r.lexStates(lexRecords, transitions)
	lang.KeywordLexStates = r.lexStates(keywordRecords, transitions)
	lang.FieldMapSlices = flatGet[[2]uint16](r)
	lang.FieldMapEntries = flatGet[FieldMapEntry](r)
	lang.AliasSequences = flatGetRows[Symbol](r)
	lang.ProductionSignatures = flatGetAttached(r, func(p *ProductionSignature, row []Symbol) { p.RHS = row })
	lang.PrimaryStateIDs = flatGet[StateID](r)
	lang.ReservedWords = flatGet[Symbol](r)
	lang.SupertypeSymbols = flatGet[Symbol](r)
	lang.SupertypeMapSlices = flatGet[[2]uint16](r)
	lang.SupertypeMapEntries = flatGet[Symbol](r)
	lang.HiddenChoicePassthroughSymbols = flatGet[bool](r)
	lang.ExternalSymbols = flatGet[Symbol](r)
	lang.ImmediateTokens = flatGet[bool](r)
	lang.ZeroWidthTokens = flatGet[bool](r)
	lang.ExternalLexStates = flatGetRows[bool](r)
	lang.NonTerminalAliasMap = flatGetRows[Symbol](r)
	if r.err == nil && r.pos != len(raw) {
		r.err = errFlatBlobCorrupt
	}
	return lang, r.err
}

func lz4DecodeChunks(dst, ends, body []byte) error {
	chunks := len(ends) / 4
	workers := min(runtime.GOMAXPROCS(0), chunks)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := w; c < chunks && errs[w] == nil; c += workers {
				lo, hi := uint32(0), binary.LittleEndian.Uint32(ends[4*c:])
				if c > 0 {
					lo = binary.LittleEndian.Uint32(ends[4*c-4:])
				}
				if lo > hi || int(hi) > len(body) {
					errs[w] = errFlatBlobCorrupt
					return
				}
				errs[w] = lz4DecodeBlock(dst[c*flatBlobChunk:min((c+1)*flatBlobChunk, len(dst))], body[lo:hi])
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func lz4DecodeBlock(dst, src []byte) error {
	d, s := 0, 0
	for s < len(src) {
		token := src[s]
		s++
		literals := int(token >> 4)
		if literals == 15 {
			n, next, ok := lz4Length(src, s)
			if !ok {
				return errFlatBlobCorrupt
			}
			literals, s = literals+n, next
		}
		switch {
		case literals <= 16 && len(src)-s >= 16 && len(dst)-d >= 16:
			binary.LittleEndian.PutUint64(dst[d:], binary.LittleEndian.Uint64(src[s:]))
			binary.LittleEndian.PutUint64(dst[d+8:], binary.LittleEndian.Uint64(src[s+8:]))
		case literals > len(src)-s || literals > len(dst)-d:
			return errFlatBlobCorrupt
		default:
			copy(dst[d:], src[s:s+literals])
		}
		d += literals
		s += literals
		if s == len(src) {
			break
		}
		if s+2 > len(src) {
			return errFlatBlobCorrupt
		}
		offset := int(src[s]) | int(src[s+1])<<8
		s += 2
		length := int(token & 15)
		if length == 15 {
			n, next, ok := lz4Length(src, s)
			if !ok {
				return errFlatBlobCorrupt
			}
			length, s = length+n, next
		}
		length += 4
		if offset == 0 || offset > d || length > len(dst)-d {
			return errFlatBlobCorrupt
		}
		start, end := d-offset, d+length
		if offset >= 8 && end+8 <= len(dst) {
			for ; d < end; d, start = d+8, start+8 {
				binary.LittleEndian.PutUint64(dst[d:], binary.LittleEndian.Uint64(dst[start:]))
			}
			d = end
			continue
		}
		for d < end {
			d += copy(dst[d:end], dst[start:d])
		}
	}
	if d != len(dst) {
		return errFlatBlobCorrupt
	}
	return nil
}

func lz4Length(src []byte, s int) (int, int, bool) {
	n := 0
	for s < len(src) {
		b := src[s]
		s++
		n += int(b)
		if b != 255 {
			return n, s, true
		}
	}
	return 0, s, false
}
