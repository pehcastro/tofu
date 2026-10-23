package host

import (
	"time"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

func (h *Host) keepCheckIn(armed *turn.LeadCheckIn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkIn = armed
}

func (h *Host) dropCheckIn() {
	if h.checking != nil {
		h.checking.Stop()
	}
	h.checkIn, h.checking = nil, nil
}

func (h *Host) armCheckIn() {
	h.mu.Lock()
	defer h.mu.Unlock()
	armed := h.checkIn
	if armed == nil || h.running || h.stopped || h.closed || h.joins {
		return
	}
	idle := func() bool { return h.checkIn == armed && !h.running && !h.closed }
	h.checking = time.AfterFunc(armed.Every, func() {
		h.mu.Lock()
		waiting := idle()
		h.mu.Unlock()
		if !waiting {
			return
		}
		text, due := armed.Due()
		h.mu.Lock()
		defer h.mu.Unlock()
		if !idle() {
			return
		}
		h.dropCheckIn()
		if due {
			h.begin(Pick{Wire: h.pick.Wire, Model: h.pick.Model, Effort: h.pick.Effort}, text, llm.Origin{Source: llm.SourceSlots})
		}
	})
}
