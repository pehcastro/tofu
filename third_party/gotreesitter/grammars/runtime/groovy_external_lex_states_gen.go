//go:build !grammar_subset || grammar_subset_groovy

package grammarruntime

func init() {
	RegisterExternalLexStates("groovy", [][]bool{
		{false, false, false, false, false, false, false, false, false, false},
		{true, true, true, true, true, true, true, true, true, true},
		{true, true, true, true, false, true, true, true, true, false},
		{true, true, true, true, true, true, true, true, true, false},
		{true, true, true, true, false, false, false, true, false, false},
		{false, true, true, true, true, true, false, false, false, false},
		{false, true, true, true, false, true, false, false, false, false},
		{true, true, true, true, false, false, true, true, false, false},
		{false, true, true, true, false, false, false, false, false, false},
		{true, true, true, true, true, false, false, false, false, false},
		{false, true, true, true, true, false, false, false, false, false},
		{true, true, true, true, false, true, true, false, false, false},
		{true, true, true, true, false, true, false, false, false, false},
		{false, true, true, true, false, false, true, false, false, false},
		{true, true, true, true, false, false, false, false, false, false},
		{false, true, true, true, false, false, false, false, false, true},
	})
}
