package parsercorephase0

import "errors"

// compareReconvergedReductionChildren is the clean subset of C parser.c's
// ts_parser__select_children. It applies only to parents built by this one
// reduction, with one identical graph predecessor. It does not authorize a
// general stack-link tie, a historical fold, or a merge between lexer owners.
// Every terminal must carry an exact scanner transition. DFA-only histories
// do not authenticate each version's lexer request and retain alternatives.
func (c *Core) compareReconvergedReductionChildren(left linkRecord, right linkInput) (int, bool, error) {
	scratch := &c.reductionScratch
	if !scratch.selectCleanChildren || left.prev != right.prev ||
		!c.reductionBatchContains(left.payload) || !c.reductionBatchContains(right.payload) {
		return 0, false, nil
	}
	l, err := c.subtree(left.payload)
	if err != nil {
		return 0, false, err
	}
	r, err := c.subtree(right.payload)
	if err != nil {
		return 0, false, err
	}
	if l.terminal || r.terminal || l.productionID != r.productionID ||
		l.dynamicPrecedence != r.dynamicPrecedence || l.symbol != r.symbol ||
		l.startByte != r.startByte || l.endByte != r.endByte || l.extra != r.extra {
		return 0, false, nil
	}
	equal, err := c.reductionTerminalHistoriesEqual(left.payload, right.payload)
	if err != nil || !equal {
		return 0, false, err
	}
	comparison, err := c.CompareCSelectionSubtrees(left.payload, right.payload)
	if errors.Is(err, ErrCSelectionComparisonBudget) {
		return 0, false, nil
	}
	return comparison, err == nil, err
}

func (c *Core) reductionBatchContains(id SubtreeID) bool {
	for _, parent := range c.reductionScratch.batchParents {
		if parent == id {
			return true
		}
	}
	return false
}

// The pop enumerator has finished before condensation starts. Its two empty
// traversal buffers can serve as independent leaf cursors without changing
// any retained pop path. Both buffers already participate in FootprintBytes.
func (c *Core) reductionTerminalHistoriesEqual(left, right SubtreeID) (bool, error) {
	scratch := &c.popScratch
	leftStack := append(scratch.rev[:0], left)
	rightStack := append(scratch.external[:0], right)
	defer func() {
		scratch.rev = leftStack[:0]
		scratch.external = rightStack[:0]
	}()
	budget := uint64(len(c.subtrees)) + uint64(len(c.children))
	var work uint64
	for {
		l, clean, err := c.nextReductionTerminal(&leftStack, &work, budget)
		if err != nil || !clean {
			return false, err
		}
		r, clean, err := c.nextReductionTerminal(&rightStack, &work, budget)
		if err != nil || !clean {
			return false, err
		}
		if l == 0 || r == 0 {
			return l == r, nil
		}
		equal, err := c.subtreesStructurallyEqual(l, r)
		if err != nil || !equal {
			return false, err
		}
		leftPrefix, leftExact := c.lexerSkippedPrefix(l)
		rightPrefix, rightExact := c.lexerSkippedPrefix(r)
		if leftExact != rightExact || leftExact && leftPrefix != rightPrefix {
			return false, nil
		}
	}
}

func (c *Core) nextReductionTerminal(stack *[]SubtreeID, work *uint64, budget uint64) (SubtreeID, bool, error) {
	for len(*stack) != 0 {
		if *work >= budget {
			return 0, false, nil
		}
		*work++
		last := len(*stack) - 1
		id := (*stack)[last]
		*stack = (*stack)[:last]
		record, err := c.subtree(id)
		if err != nil {
			return 0, false, err
		}
		// C reserves the last two symbol IDs for ERROR and ERROR_REPEAT.
		if record.missing || record.symbol >= ErrorRegionSymbol-1 ||
			record.externalProvenanceState == subtreeExternalProvenanceReusedOpaque {
			return 0, false, nil
		}
		if record.terminal {
			if !record.external {
				return 0, false, nil
			}
			if record.external {
				provenance, exact := c.externalPayloadScannerProvenance(id)
				if !exact {
					return 0, false, nil
				}
				for _, checkpoint := range [...]CheckpointID{provenance.start, provenance.end} {
					if checkpoint != 0 {
						if _, ok := c.checkpoints.record(checkpoint); !ok {
							return 0, false, errors.New("parser-core phase zero: reduction terminal checkpoint is unavailable")
						}
					}
				}
			}
			return id, true, nil
		}
		for index := record.childCount; index > 0; index-- {
			child := c.children[record.firstChild+index-1]
			if child == 0 || child >= id {
				return 0, false, errors.New("parser-core phase zero: reduction terminal child identifier is out of order")
			}
			*stack = append(*stack, child)
		}
	}
	return 0, true, nil
}
