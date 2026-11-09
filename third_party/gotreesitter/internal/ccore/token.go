package ccore

import "bytes"

func (p *parser) cached(v int, state uint16, position uint32) (uint32, *ActionEntry) {
	if p.cacheToken == 0 || p.cacheByte != position || !p.r.sameScannerState(p.cacheExternal, p.s.heads[v].external) {
		return 0, nil
	}
	tok := &p.r.Trees[p.cacheToken]
	entry := p.t.entry(state, tok.Symbol)
	current, leafMode := p.t.LexModes[state], p.t.LexModes[tok.parseState()]
	if current.LexState == noLexState {
		return 0, nil
	}
	if len(entry.Actions) > 0 && leafMode == current && (tok.Symbol != p.t.KeywordCapture || tok.kind != keywordToken && tok.parseState() == state) {
		return p.cacheToken, entry
	}
	if tok.Size == 0 && tok.Symbol != symEnd {
		return 0, nil
	}
	if current.External == 0 && entry.Reusable {
		return p.cacheToken, entry
	}
	return 0, nil
}

func (p *parser) lex(v int, parseState uint16) uint32 {
	mode := p.t.LexModes[parseState]
	if mode.LexState == noLexState {
		return 0
	}
	l := &p.l
	start := p.s.node(v).position
	errorMode := parseState == errorState
	skipped := false
	var errStart, errEnd uint32
	l.reset(start)
	for {
		if mode.External != 0 {
			if leaf := p.scan(start, l.pos, mode.External, v, parseState, errorMode); leaf != 0 {
				if !skipped {
					return leaf
				}
				break
			}
		}
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
		if l.pos == errEnd {
			if l.eof() {
				break
			}
			l.advance(false)
		}
		errEnd = l.pos
	}
	if skipped {
		return p.r.leaf(symError, errStart-start, errEnd-errStart, parseState, false)
	}
	symbol := l.result
	padding, size := l.tokenStart-start, l.tokenEnd-l.tokenStart
	keyword := false
	if symbol == p.t.KeywordCapture && symbol != 0 {
		end := l.tokenEnd
		l.reset(l.tokenStart)
		l.start()
		keyword = l.run(&p.t.Keywords, 0)
		if keyword && l.tokenEnd == end && (p.t.lookup(parseState, l.result) != 0 || p.t.isReserved(parseState, l.result)) {
			symbol = l.result
		}
	}
	return p.r.leaf(symbol, padding, size, parseState, keyword)
}

func (p *parser) scan(start, current uint32, external uint16, v int, parseState uint16, errorMode bool) uint32 {
	at := current
	if at == 0 && bytes.HasPrefix(p.l.src, []byte(byteOrderMark)) {
		at = uint32(len(byteOrderMark))
	}
	last := p.s.heads[v].external
	previous := p.r.scannerState(last)
	symbol, tokenStart, tokenEnd, found := p.scanner.Scan(at, p.t.ExternalStates[external], previous)
	if !found {
		return 0
	}
	state := p.scanBuf[:p.scanner.Serialize(p.scanBuf[:])]
	changed := !bytes.Equal(previous, state)
	if tokenEnd <= current && !changed && (errorMode || p.t.nextState(parseState, symbol) == parseState) {
		return 0
	}
	scan := p.r.Trees[last].scan()
	if changed || scan == 0 {
		scan = p.r.addScan(state)
	}
	leaf := p.r.leaf(symbol, tokenStart-start, tokenEnd-tokenStart, parseState, false)
	p.r.Trees[leaf].kidsOrScan = scan
	return leaf
}
