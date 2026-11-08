package parsercorephase0

import "slices"

// AcceptanceMaterializationPayloadLimit bounds retained selection metadata.
// Wider selections use the existing comparison and publication path.
const AcceptanceMaterializationPayloadLimit = 8

// AcceptanceMaterialization owns a completed publication while a tied
// acceptance is checked. It transfers that publication once, only for the
// exact core generation, head, payload sequence, and materialization context.
// Discard releases a publication that cannot be returned to its caller.
type AcceptanceMaterialization[T interface{ Release() }] struct {
	value      T
	compact    *Core
	generation uint64
	phase      uint64
	head       Head
	context    uint64
	payloads   []SubtreeID
	inline     [AcceptanceMaterializationPayloadLimit]SubtreeID
	present    bool
}

// Store takes ownership only when the slot is empty and the selection is
// complete. The payload identity is copied; caller scratch cannot alter it.
func (a *AcceptanceMaterialization[T]) Store(compact *Core, head Head, payloads []SubtreeID, context uint64, value T) bool {
	if a == nil || a.present || compact == nil || head.Node == 0 || len(payloads) == 0 ||
		len(payloads) > len(a.inline) {
		return false
	}
	a.compact, a.generation, a.head, a.context = compact, compact.ResetGeneration(), head, context
	a.phase = compact.AuthenticationGeneration()
	a.payloads = a.inline[:len(payloads)]
	copy(a.payloads, payloads)
	a.value, a.present = value, true
	return true
}

// Take leaves a mismatched publication owned by the slot, for Discard.
func (a *AcceptanceMaterialization[T]) Take(compact *Core, head Head, payloads []SubtreeID, context uint64) (T, bool) {
	if a == nil || !a.present || compact == nil || a.compact != compact ||
		a.generation != compact.ResetGeneration() || a.phase != compact.AuthenticationGeneration() ||
		a.head != head || a.context != context ||
		!slices.Equal(a.payloads, payloads) {
		var zero T
		return zero, false
	}
	value := a.value
	a.clear()
	return value, true
}

func (a *AcceptanceMaterialization[T]) Discard() {
	if a == nil || !a.present {
		return
	}
	value := a.value
	a.clear()
	value.Release()
}

// Retained returns the owned value for memory accounting, without transferring
// ownership. It must never be published or released by this reader.
func (a *AcceptanceMaterialization[T]) Retained() T {
	if a == nil || !a.present {
		var zero T
		return zero
	}
	return a.value
}

func (a *AcceptanceMaterialization[T]) clear() {
	var zero T
	a.value = zero
	a.compact = nil
	a.generation, a.phase, a.context = 0, 0, 0
	a.head = Head{}
	clear(a.inline[:])
	a.payloads = nil
	a.present = false
}
