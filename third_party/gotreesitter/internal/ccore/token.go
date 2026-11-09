package ccore

func (p *parser) cached(state uint16, position uint32) (uint32, *ActionEntry) {
	if p.cacheToken == 0 || p.cacheByte != position {
		return 0, nil
	}
	tok := &p.r.Trees[p.cacheToken]
	entry := p.t.entry(state, tok.Symbol)
	current, leafMode := p.t.LexModes[state], p.t.LexModes[tok.ParseState]
	if current.LexState == noLexState {
		return 0, nil
	}
	if len(entry.Actions) > 0 && leafMode == current && (tok.Symbol != p.t.KeywordCapture || !tok.Keyword && tok.ParseState == state) {
		return p.cacheToken, entry
	}
	if tok.Size.Bytes == 0 && tok.Symbol != symEnd {
		return 0, nil
	}
	if entry.Reusable {
		return p.cacheToken, entry
	}
	return 0, nil
}

func (p *parser) lex(v int, parseState uint16) uint32 {
	l := &p.l
	mode := p.t.LexModes[parseState]
	start := p.s.node(v).position
	errorMode := parseState == errorState
	skipped := false
	var errStart, errEnd Length
	l.reset(start)
	for {
		l.start()
		found := l.run(&p.t.Main, int32(mode.LexState))
		l.finish()
		if found {
			break
		}
		if !errorMode {
			errorMode = true
			mode = p.t.LexModes[errorState]
			l.reset(start)
			continue
		}
		if !skipped {
			skipped = true
			errStart, errEnd = l.tokenStart, l.tokenStart
		}
		if l.pos.Bytes == errEnd.Bytes {
			if l.eof() {
				break
			}
			l.advance(false)
		}
		errEnd = l.pos
	}
	if skipped {
		return p.r.leaf(symError, errStart.sub(start), errEnd.sub(errStart), parseState, false)
	}
	symbol := l.result
	padding, size := l.tokenStart.sub(start), l.tokenEnd.sub(l.tokenStart)
	keyword := false
	if symbol == p.t.KeywordCapture && symbol != 0 {
		end := l.tokenEnd.Bytes
		l.reset(l.tokenStart)
		l.start()
		keyword = l.run(&p.t.Keywords, 0)
		if keyword && l.tokenEnd.Bytes == end && (p.t.lookup(parseState, l.result) != 0 || p.t.isReserved(parseState, l.result)) {
			symbol = l.result
		}
	}
	return p.r.leaf(symbol, padding, size, parseState, keyword)
}
