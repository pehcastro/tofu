//go:build gts_parsercorephase0

package parsercorephase0

import "sync/atomic"

var externalLexerOwnershipProbe atomic.Bool

// ExternalLexerOwnershipProbeEnabled keeps early full-external activation
// diagnostic-only until its complete-parse throughput clears the route gate.
func ExternalLexerOwnershipProbeEnabled() bool {
	return externalLexerOwnershipProbe.Load()
}

// SetExternalLexerOwnershipProbeForTest arms one serial test or benchmark.
// The returned restore function also supports nested, serial probe scopes.
func SetExternalLexerOwnershipProbeForTest(enabled bool) func() {
	previous := externalLexerOwnershipProbe.Swap(enabled)
	return func() { externalLexerOwnershipProbe.Store(previous) }
}
