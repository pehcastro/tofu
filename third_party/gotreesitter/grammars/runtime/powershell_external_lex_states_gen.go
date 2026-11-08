//go:build !grammar_subset || grammar_subset_powershell

package grammarruntime

var powershellExternalLexStates = [][]bool{
	{false, false},
	{true, true},
	{true, false},
	{false, true},
}

func init() {
	RegisterExternalLexStates("powershell", powershellExternalLexStates)
}
