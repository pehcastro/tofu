//go:build !grammar_subset || grammar_subset_c_sharp

package grammarruntime

var cSharpExternalLexStates = [][]bool{
	/*  0 */ {false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
	/*  1 */ {true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
	/*  2 */ {false, true, true, true, false, false, false, false, false, true, false, false, true, true, true, false},
	/*  3 */ {false, true, true, true, false, false, false, true, false, true, false, false, true, true, true, false},
	/*  4 */ {false, false, false, false, false, false, false, false, false, false, false, false, false, true, true, false},
	/*  5 */ {false, false, false, false, false, false, false, false, false, false, false, false, true, true, true, false},
	/*  6 */ {false, false, false, false, false, false, false, true, false, false, false, false, true, true, true, false},
	/*  7 */ {false, false, false, false, false, false, false, true, false, false, false, false, false, true, true, false},
	/*  8 */ {false, false, false, false, false, false, false, false, false, false, false, false, false, true, true, true},
	/*  9 */ {false, false, false, false, false, true, true, false, true, false, false, false, false, true, true, false},
	{true, false, false, false, false, false, false, false, false, false, false, false, false, true, true, false},
	{false, false, false, false, true, false, false, false, false, false, false, false, false, true, true, false},
	{false, false, false, false, false, false, false, false, false, false, true, false, false, true, true, false},
	{false, false, false, false, false, false, false, false, false, false, false, true, false, true, true, false},
}

func init() {
	RegisterExternalLexStates("c_sharp", cSharpExternalLexStates)
}
