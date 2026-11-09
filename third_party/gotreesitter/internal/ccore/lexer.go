package ccore

import "unicode/utf8"

type lexer struct {
	src        []byte
	pos        uint32
	lookahead  int32
	size       uint32
	tokenStart uint32
	tokenEnd   uint32
	endMarked  bool
	result     uint16
}

func (l *lexer) eof() bool { return int(l.pos) >= len(l.src) }

func (l *lexer) decode() {
	if l.eof() {
		l.lookahead, l.size = 0, 1
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
