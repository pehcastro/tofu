//go:build !grammar_subset || grammar_subset_cpp

package grammarruntime

var cppExternalLexStates = [][]bool{
	{false, false, false, false, false, false, false, false},
	{true, true, true, true, true, true, true, true},
	{false, false, true, true, false, true, false, false},
	{false, false, true, true, false, true, true, false},
	{false, false, true, true, false, false, false, false},
	{false, false, true, true, false, true, false, true},
	{false, false, true, true, false, false, true, false},
	{false, false, true, true, true, false, false, false},
	{true, false, true, true, false, false, false, false},
	{false, true, true, true, false, false, false, false},
}

func init() {
	RegisterExternalLexStates("cpp", cppExternalLexStates)
}
