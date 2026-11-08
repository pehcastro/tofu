//go:build !grammar_subset || grammar_subset_sql

package grammarruntime

var sqlExternalLexStates = [][]bool{
	{false, false, false, false, false, false, false},
	{true, true, true, true, true, true, true},
	{false, false, false, false, true, false, false},
	{false, false, true, false, false, false, false},
	{false, false, false, false, true, true, true},
	{false, true, false, false, false, false, false},
	{false, false, true, false, true, true, true},
	{false, true, true, false, false, false, false},
	{true, false, false, false, false, false, false},
	{false, false, false, true, false, false, false},
}

func init() {
	RegisterExternalLexStates("sql", sqlExternalLexStates)
}
