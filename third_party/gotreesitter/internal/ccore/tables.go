package ccore

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
}

type Tables struct {
	SymbolCount        uint32
	TokenCount         uint32
	LargeStateCount    uint32
	ParseTable         []uint16
	SmallParseTable    []uint16
	SmallParseTableMap []uint32
	Actions            []ActionEntry
	LexModes           []LexMode
	Main               Lexer
	Keywords           Lexer
	KeywordCapture     uint16
	ReservedWords      []uint16
	ReservedSetSize    uint32
	ExternalStates     [][]bool
}

type Scanner interface {
	Scan(at Length, valid []bool, state []byte) (symbol uint16, start, end Length, found bool)
	Serialize(buf []byte) int
}

const (
	symEnd         uint16 = 0
	symError       uint16 = 65535
	errorState     uint16 = 0
	noLexState     uint16 = 0xFFFF
	invalidRune    int32  = -1
	dfaInvalidRune int32  = 0x10FFFF
	byteOrderMark         = "\xef\xbb\xbf"
)

func (t *Tables) lookup(state, symbol uint16) uint16 {
	if uint32(state) < t.LargeStateCount {
		return t.ParseTable[uint32(state)*t.SymbolCount+uint32(symbol)]
	}
	data := t.SmallParseTable[t.SmallParseTableMap[uint32(state)-t.LargeStateCount]:]
	groups := int(data[0])
	i := 1
	for g := 0; g < groups; g++ {
		value, count := data[i], int(data[i+1])
		i += 2
		for _, s := range data[i : i+count] {
			if s == symbol {
				return value
			}
		}
		i += count
	}
	return 0
}

func (t *Tables) entry(state, symbol uint16) *ActionEntry {
	if symbol == symError {
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
