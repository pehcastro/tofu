package ccore

import "unicode/utf8"

type Length struct {
	Bytes  uint32
	Row    uint32
	Column uint32
}

func (a Length) Add(b Length) Length {
	if b.Row > 0 {
		return Length{a.Bytes + b.Bytes, a.Row + b.Row, b.Column}
	}
	return Length{a.Bytes + b.Bytes, a.Row, a.Column + b.Column}
}

func (a Length) sub(b Length) Length {
	if a.Row > b.Row {
		return Length{a.Bytes - b.Bytes, a.Row - b.Row, a.Column}
	}
	return Length{a.Bytes - b.Bytes, 0, a.Column - b.Column}
}

type lexer struct {
	src        []byte
	pos        Length
	lookahead  int32
	size       uint32
	tokenStart Length
	tokenEnd   Length
	endMarked  bool
	result     uint16
}

func (l *lexer) eof() bool { return int(l.pos.Bytes) >= len(l.src) }

func (l *lexer) decode() {
	if l.eof() {
		l.lookahead, l.size = 0, 1
		return
	}
	r, n := utf8.DecodeRune(l.src[l.pos.Bytes:])
	if r == utf8.RuneError && n <= 1 {
		l.lookahead, l.size = invalidRune, 1
		return
	}
	l.lookahead, l.size = r, uint32(n)
}

func (l *lexer) reset(p Length) {
	if p.Bytes != l.pos.Bytes {
		l.pos = p
		l.lookahead, l.size = 0, 0
	}
}

func (l *lexer) advance(skip bool) {
	if l.eof() {
		return
	}
	if l.lookahead == '\n' {
		l.pos.Row++
		l.pos.Column = 0
	} else {
		l.pos.Column += l.size
	}
	l.pos.Bytes += l.size
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
	if l.pos.Bytes == 0 && l.lookahead == 0xFEFF {
		l.advance(true)
	}
}

func (l *lexer) finish() {
	if !l.endMarked {
		l.tokenEnd = l.pos
		l.endMarked = true
	}
	if l.tokenEnd.Bytes < l.tokenStart.Bytes {
		l.tokenStart = l.tokenEnd
	}
}

func (l *lexer) run(dfa *Lexer, state int32) bool {
	found := false
	for {
		st := &dfa.States[state]
		if st.Accepts {
			found = true
			l.result = st.AcceptToken
			l.tokenEnd = l.pos
			l.endMarked = true
		}
		if st.EOF >= 0 && l.eof() {
			state = st.EOF
			continue
		}
		c := l.lookahead
		if c == invalidRune {
			c = dfaInvalidRune
		}
		next := int32(-1)
		for _, t := range dfa.Transitions[st.TransStart:st.TransEnd] {
			if t.Lo <= c && c <= t.Hi {
				next = t.Next
				l.advance(t.Skip)
				break
			}
		}
		if next < 0 {
			return found
		}
		state = next
	}
}
