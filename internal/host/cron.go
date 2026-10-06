package host

import (
	"context"
	"slices"
	"time"

	"tofu/internal/cron"
	"tofu/internal/konst"
)

func (h *Host) CronCommand(line string) (cron.Reply, error) {
	h.mu.Lock()
	readOnly, idle := h.readOnly, !h.running
	h.mu.Unlock()
	if readOnly != nil {
		return cron.Reply{}, readOnly
	}
	reply, err := h.cron.Command(line, h.now())
	if err != nil {
		return reply, err
	}
	if reply.CheckGoals && idle {
		go h.fire(h.cron.Due(context.Background(), h.now(), cron.TurnEnded))
	}
	h.armCron()
	return reply, nil
}

func (h *Host) armCron() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ticking != nil || h.closed || h.readOnly != nil || h.cron.Live() == 0 {
		return
	}
	h.ticking = time.AfterFunc(konst.CronPollMillis*time.Millisecond, func() {
		h.mu.Lock()
		h.ticking = nil
		h.mu.Unlock()
		h.fire(h.cron.Due(context.Background(), h.now(), cron.Tick))
		h.armCron()
	})
}

func (h *Host) fire(fires []cron.Fire) {
	for _, fired := range fires {
		h.note(fired.Line)
		if fired.Prompt == "" {
			continue
		}
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		h.fired = append(h.fired, fired.ID)
		if !h.running {
			h.begin(Pick{Wire: h.pick.Wire, Model: h.pick.Model, Effort: h.pick.Effort, Fired: fired.ID}, fired.Prompt)
			h.mu.Unlock()
			continue
		}
		h.unread = append(h.unread, fired)
		h.mu.Unlock()
		h.Steer(fired.Prompt)
	}
}

func (h *Host) markRead(steered string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unread = slices.DeleteFunc(h.unread, func(fired cron.Fire) bool { return fired.Prompt == steered })
}

func (h *Host) ended(out *emitter) {
	h.mu.Lock()
	stopped, fired, unread, answer := h.stopped, h.fired, h.unread, h.answer
	h.running, h.cancel, h.fired, h.unread = false, nil, nil, nil
	h.mu.Unlock()
	out.emit(Event{Kind: EventTurnEnded})
	for _, id := range fired {
		if slices.ContainsFunc(unread, func(again cron.Fire) bool { return again.ID == id }) {
			continue
		}
		if note := h.cron.Finished(id, answer); note != "" {
			h.note(note)
		}
	}
	for index := range unread {
		unread[index].Line = unread[index].ID + " fired during a turn that ended before reading it, so it starts its own"
	}
	h.fire(unread)
	if !stopped {
		go h.fire(h.cron.Due(context.Background(), h.now(), cron.TurnEnded))
	}
	h.armCron()
}
