package ccore

const (
	InvalidRune    = invalidRune
	DFAInvalidRune = dfaInvalidRune
)

func (dfa *Lexer) Step(state, c int32) (int32, bool) { return dfa.step(state, c) }

func (dfa *Lexer) Scan(state, c int32) (int32, bool) { return dfa.scan(state, c) }

func (dfa *Lexer) Classes() []uint8 { return dfa.classes[:] }

func (dfa *Lexer) DenseBytes() int { return 2 * len(dfa.dense) }

func (t *Tables) Lookup(state, symbol uint16) uint16 { return t.lookup(state, symbol) }

func (s *SmallTable) Bytes() int { return 8*len(s.words) + 4*len(s.ranks) + 2*len(s.values) }
