package trace

import "testing"

func TestShortWritesTheLastSixCharactersWithAHashInFront(t *testing.T) {
	if got := Short("a3f9c1de2233"); got != "#de2233" {
		t.Fatalf("Short(%q) = %q, want #de2233", "a3f9c1de2233", got)
	}
}

func TestShortDistinguishesTwoIDsThatShareALiteralPrefix(t *testing.T) {
	first, second := Short("turn-18d6d2c8a10c7b60"), Short("turn-18d6de33a2c09dbc")
	if first == second {
		t.Fatalf("two different session ids both shortened to %q", first)
	}
}

func TestShortOfAShortIDDoesNotPad(t *testing.T) {
	if got := Short("ab"); got != "#ab" {
		t.Fatalf("Short(%q) = %q, want #ab", "ab", got)
	}
}

func TestShortOfAnEmptyIDIsEmpty(t *testing.T) {
	if got := Short(""); got != "" {
		t.Fatalf("Short(\"\") = %q, want empty", got)
	}
}
