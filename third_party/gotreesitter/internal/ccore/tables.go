package ccore

import (
	"math/bits"
	"unicode/utf8"
)

type ActionType uint8

const (
	ActionShift ActionType = iota
	ActionReduce
	ActionAccept
	ActionRecover
)

type Action struct {
	Type         ActionType
	Extra        bool
	Repetition   bool
	ChildCount   uint8
	Symbol       uint16
	State        uint16
	ProductionID uint16
	DynPrec      int16
}

type ActionEntry struct {
	Reusable bool
	Actions  []Action
}

type LexState struct {
	Accepts     bool
	AcceptToken uint16
	EOF         int32
	TransStart  uint32
	TransEnd    uint32
}

type LexTransition struct {
	Lo, Hi int32
	Next   int32
	Skip   bool
}

type LexMode struct {
	LexState    uint16
	External    uint16
	ReservedSet uint16
}

type Lexer struct {
	States      []LexState
	Transitions []LexTransition
	classes     [utf8.RuneSelf]uint8
	classCount  uint32
	dense       []denseEntry
}

type Tables struct {
	SymbolCount     uint32
	TokenCount      uint32
	LargeStateCount uint32
	ParseTable      []uint16
	Small           SmallTable
	Actions         []ActionEntry
	LexModes        []LexMode
	Main            Lexer
	Keywords        Lexer
	KeywordCapture  uint16
	ReservedWords   []uint16
	ReservedSetSize uint32
	ExternalStates  [][]bool
	Visible         []bool
	Aliases         [][]uint16
	ErrorSymbol     uint16
}

func (t *Tables) errorSymbol() uint16 { return t.ErrorSymbol }

func (t *Tables) errorRepeatSymbol() uint16 { return t.ErrorSymbol + 1 }

func (t *Tables) aliases(production uint16) []uint16 {
	if production == 0 || int(production) >= len(t.Aliases) {
		return nil
	}
	return t.Aliases[production]
}

func (t *Tables) hasReduce(state, symbol uint16) bool {
	actions := t.entry(state, symbol).Actions
	return len(actions) > 0 && actions[0].Type == ActionReduce
}

type Scanner interface {
	Scan(at uint32, valid []bool, state []byte) (symbol uint16, start, end uint32, found bool)
	Serialize(buf []byte) int
}

const (
	symEnd         uint16 = 0
	errorState     uint16 = 0
	noLexState     uint16 = 0xFFFF
	invalidRune    int32  = -1
	dfaInvalidRune int32  = 0x10FFFF
	byteOrderMark         = "\xef\xbb\xbf"
)

type SmallTable struct {
	width  uint32
	words  []uint64
	ranks  []uint32
	values []uint16
}

func NewSmallTable(symbolCount uint32, table []uint16, offsets []uint32) SmallTable {
	width := (symbolCount + 63) / 64
	s := SmallTable{width: width, words: make([]uint64, uint32(len(offsets))*width), ranks: make([]uint32, uint32(len(offsets))*width)}
	row := make([]uint16, width*64)
	for k, offset := range offsets {
		base := uint32(k) * width
		words, ranks := s.words[base:base+width], s.ranks[base:base+width]
		data := table[offset:]
		i := 1
		for range int(data[0]) {
			value, count := data[i], int(data[i+1])
			for _, symbol := range data[i+2 : i+2+count] {
				if bit := uint64(1) << (symbol & 63); words[symbol>>6]&bit == 0 {
					words[symbol>>6] |= bit
					row[symbol] = value
				}
			}
			i += 2 + count
		}
		for w, word := range words {
			ranks[w] = uint32(len(s.values))
			for ; word != 0; word &= word - 1 {
				s.values = append(s.values, row[w*64+bits.TrailingZeros64(word)])
			}
		}
	}
	return s
}

func (s *SmallTable) lookup(index uint32, symbol uint16) uint16 {
	at := index*s.width + uint32(symbol>>6)
	word, bit := s.words[at], uint64(1)<<(symbol&63)
	if word&bit == 0 {
		return 0
	}
	return s.values[s.ranks[at]+uint32(bits.OnesCount64(word&(bit-1)))]
}

func (t *Tables) lookup(state, symbol uint16) uint16 {
	if uint32(state) < t.LargeStateCount {
		return t.ParseTable[uint32(state)*t.SymbolCount+uint32(symbol)]
	}
	return t.Small.lookup(uint32(state)-t.LargeStateCount, symbol)
}

func (t *Tables) entry(state, symbol uint16) *ActionEntry {
	if uint32(symbol) >= t.SymbolCount {
		return &t.Actions[0]
	}
	return &t.Actions[t.lookup(state, symbol)]
}

func (t *Tables) nextState(state, symbol uint16) uint16 {
	if uint32(symbol) < t.TokenCount {
		actions := t.Actions[t.lookup(state, symbol)].Actions
		if n := len(actions); n > 0 && actions[n-1].Type == ActionShift {
			if actions[n-1].Extra {
				return state
			}
			return actions[n-1].State
		}
		return 0
	}
	return t.lookup(state, symbol)
}

func (t *Tables) isReserved(state, symbol uint16) bool {
	set := uint32(t.LexModes[state].ReservedSet)
	if set == 0 {
		return false
	}
	for _, w := range t.ReservedWords[set*t.ReservedSetSize : (set+1)*t.ReservedSetSize] {
		if w == symbol {
			return true
		}
		if w == 0 {
			break
		}
	}
	return false
}
