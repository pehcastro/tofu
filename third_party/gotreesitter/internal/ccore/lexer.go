package ccore

import (
	"unicode/utf8"
	"unsafe"
)

type lexer struct {
	src        []byte
	pos        uint32
	lookahead  int32
	size       uint32
	tokenStart uint32
	tokenEnd   uint32
	endMarked  bool
	eofCycled  bool
	result     uint16
}

func (l *lexer) eof() bool { return int(l.pos) >= len(l.src) }

func (l *lexer) decode() {
	if l.eof() {
		l.lookahead, l.size = 0, 1
		return
	}
	if b := l.src[l.pos]; b < utf8.RuneSelf {
		l.lookahead, l.size = int32(b), 1
		return
	}
	r, n := utf8.DecodeRune(l.src[l.pos:])
	if r == utf8.RuneError && n <= 1 {
		l.lookahead, l.size = invalidRune, 1
		return
	}
	l.lookahead, l.size = r, uint32(n)
}

func (l *lexer) reset(p uint32) {
	if p != l.pos {
		l.pos = p
		l.lookahead, l.size = 0, 0
	}
}

func (l *lexer) advance(skip bool) {
	if l.eof() {
		return
	}
	l.pos += l.size
	if skip {
		l.tokenStart = l.pos
	}
	l.decode()
}

func (l *lexer) start() {
	l.tokenStart = l.pos
	l.endMarked = false
	l.result = 0
	if l.eof() {
		return
	}
	if l.size == 0 {
		l.decode()
	}
	if l.pos == 0 && l.lookahead == 0xFEFF {
		l.advance(true)
	}
}

func (l *lexer) finish() {
	if !l.endMarked {
		l.tokenEnd = l.pos
		l.endMarked = true
	}
	if l.tokenEnd < l.tokenStart {
		l.tokenStart = l.tokenEnd
	}
}

func (l *lexer) run(dfa *Lexer, state int32) bool {
	found := false
	eofHops := 0
	for {
		st := &dfa.States[state]
		if st.Accepts {
			found = true
			l.result = st.AcceptToken
			l.tokenEnd = l.pos
			l.endMarked = true
		}
		if st.EOF >= 0 && l.eof() {
			if eofHops++; eofHops > len(dfa.States) {
				l.eofCycled = true
				return found
			}
			state = st.EOF
			continue
		}
		next, skip := dfa.step(state, l.lookahead)
		if next < 0 || l.lookahead == 0 && l.eof() {
			return found
		}
		l.advance(skip)
		state = next
	}
}

type denseEntry uint16

const maxDenseStates = 1<<15 - 1

func toDense(next int32, skip bool) denseEntry {
	e := denseEntry(next+1) << 1
	if skip {
		e |= 1
	}
	return e
}

func (e denseEntry) next() (int32, bool) { return int32(e>>1) - 1, e&1 != 0 }

func (dfa *Lexer) step(state, c int32) (int32, bool) {
	if uint32(c) < utf8.RuneSelf {
		return dfa.dense[int(state)*int(dfa.classCount)+int(dfa.classes[c])].next()
	}
	return dfa.scan(state, c)
}

func (dfa *Lexer) scan(state, c int32) (int32, bool) {
	if c == invalidRune {
		return dfa.dense[(int(state)+1)*int(dfa.classCount)-1].next()
	}
	st := &dfa.States[state]
	for _, t := range dfa.Transitions[st.TransStart:st.TransEnd] {
		if t.Lo <= c && c <= t.Hi {
			return t.Next, t.Skip
		}
	}
	return -1, false
}

const largeCharacterRangeCount = 8

func invalidRuneColumn(states []LexState, transitions []LexTransition) []denseEntry {
	type negatedSet struct {
		state int
		first LexTransition
	}
	var sets []negatedSet
	large := map[int32]bool{}
	for s, st := range states {
		trs := transitions[st.TransStart:st.TransEnd]
		for start := 0; start < len(trs); {
			first, end, negated := trs[start], start, false
			for end < len(trs) && trs[end].Next == first.Next && trs[end].Skip == first.Skip {
				negated = negated || trs[end].Hi == utf8.MaxRune
				end++
			}
			if negated {
				sets = append(sets, negatedSet{s, first})
				large[first.Next] = large[first.Next] || end-start >= largeCharacterRangeCount
			}
			start = end
		}
	}
	column := make([]denseEntry, len(states))
	for i := len(sets) - 1; i >= 0; i-- {
		if set := sets[i]; !large[set.first.Next] {
			column[set.state] = toDense(set.first.Next, set.first.Skip)
		}
	}
	return column
}

func NewLexer(states []LexState, transitions []LexTransition) Lexer {
	if len(states) > maxDenseStates {
		panic("ccore: lex states do not fit a dense entry")
	}
	dfa := Lexer{States: states, Transitions: transitions}
	var boundary [utf8.RuneSelf + 1]bool
	for _, t := range transitions {
		boundary[min(max(t.Lo, 0), utf8.RuneSelf)] = true
		boundary[min(max(t.Hi+1, 0), utf8.RuneSelf)] = true
	}
	seen := map[string]uint8{}
	var columns [][]denseEntry
	column := make([]denseEntry, len(states))
	for b := range dfa.classes {
		if b > 0 && !boundary[b] {
			dfa.classes[b] = dfa.classes[b-1]
			continue
		}
		for s := range states {
			column[s] = toDense(dfa.scan(int32(s), int32(b)))
		}
		key := unsafe.String((*byte)(unsafe.Pointer(unsafe.SliceData(column))), 2*len(column))
		class, ok := seen[key]
		if !ok {
			class = uint8(len(columns))
			seen[key] = class
			columns = append(columns, column)
			column = make([]denseEntry, len(states))
		}
		dfa.classes[b] = class
	}
	columns = append(columns, invalidRuneColumn(states, transitions))
	dfa.classCount = uint32(len(columns))
	dfa.dense = make([]denseEntry, len(states)*len(columns))
	for k, col := range columns {
		for s, e := range col {
			dfa.dense[s*len(columns)+k] = e
		}
	}
	return dfa
}
