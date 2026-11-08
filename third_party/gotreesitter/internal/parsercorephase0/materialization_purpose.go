package parsercorephase0

// MaterializationPurpose separates published trees from temporary acceptance
// comparisons. Both construct the same syntax and run the same completeness
// checks. Comparison trees omit replay stamps and scanner checkpoint copies
// used only for incremental reuse. They preserve the original acceptance
// trials' states-free traversal, are never published, and cannot be reused.
type MaterializationPurpose uint8

const (
	MaterializationForPublication MaterializationPurpose = iota
	MaterializationForComparison
)
