package schemas

const DeferredMechanismName = "a tool_definition fetch: tofu sends a one-line name and one-sentence purpose for every tool up front, adds one new tool named tool_definition(name), and the model must call it once for each tool whose full schema it has not yet fetched, before that tool's first real use"

const CatalogTokensPerToolEstimate = 20

type DeferredCost struct {
	DistinctToolsUsed  int
	ExtraRoundTrips    int
	CatalogTokens      int
	UnusedSchemaTokens int
}

func CostOfDeferring(whole Whole, calledNames []string, totalToolCount int) DeferredCost {
	used := len(calledNames)
	usedTokens := 0
	byName := map[string]int{}
	for _, tc := range whole.Tools {
		byName[tc.Name] = tc.Tokens
	}
	for _, name := range calledNames {
		usedTokens += byName[name]
	}
	return DeferredCost{
		DistinctToolsUsed:  used,
		ExtraRoundTrips:    used,
		CatalogTokens:      totalToolCount * CatalogTokensPerToolEstimate,
		UnusedSchemaTokens: whole.SumTokens - usedTokens,
	}
}
