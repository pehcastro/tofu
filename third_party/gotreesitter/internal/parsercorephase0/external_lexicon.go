package parsercorephase0

// ExternalLexerOwnsAllTerminals proves that every non-EOF terminal is supplied
// by the external scanner. Such a lexer must see each competing parser state's
// valid-symbol set before shared lookahead can merge their reduction paths.
func ExternalLexerOwnsAllTerminals[S ~uint16](tokenCount uint32, symbols []S) bool {
	if tokenCount < 2 || tokenCount > 1<<16 || uint32(len(symbols))+1 != tokenCount {
		return false
	}
	ordered := true
	for index, symbol := range symbols {
		if uint32(symbol) != uint32(index)+1 {
			ordered = false
			break
		}
	}
	if ordered {
		return true
	}
	seen := make([]bool, tokenCount)
	for _, symbol := range symbols {
		if symbol == 0 || uint32(symbol) >= tokenCount || seen[symbol] {
			return false
		}
		seen[symbol] = true
	}
	return true
}
