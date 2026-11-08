package parsercorephase0

// condenseNodeHasIndependentOwner distinguishes a live, incompatible lexer
// version from a dead boundary. Treating both as non-candidates would import
// historical alternatives from a still-live version into the current output.
func (c *Core) condenseNodeHasIndependentOwner(id NodeID) bool {
	if !c.condenseScopeActive || id >= c.condenseNewNode {
		return false
	}
	for _, candidate := range c.condenseCandidates {
		if candidate.Head.Node == id {
			return candidate.Independent
		}
	}
	return false
}

// CanonicalVersionRemap requires a live version with the same immutable lexer
// identity to witness an owned-header remap. A boundary's parser state, byte,
// and scanner checkpoint alone do not identify its pending lexer request.
// Unowned headers retain the shared-lexer canonicalization rule.
func CanonicalVersionRemap[T any](current, canonical Head, owned bool, versions []T, matches func(T, Head) bool) bool {
	if !owned || current == canonical {
		return true
	}
	for _, version := range versions {
		if matches(version, canonical) {
			return true
		}
	}
	return false
}
