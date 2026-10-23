package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"tofu/internal/cron"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/safe"
	"tofu/internal/session"
)

func (h *Host) CronCommand(line string) (cron.Reply, error) {
	if err := h.reachHost(); err != nil {
		return cron.Reply{}, err
	}
	if f := h.following.Load(); f != nil {
		reply := f.ask(followAsk{Op: followCron, Text: line})
		switch {
		case reply.ID == 0:
			return cron.Reply{}, errHostSilent
		case reply.Failed != "":
			return cron.Reply{}, errors.New(reply.Failed)
		}
		return reply.Cron, h.loadCron(h.ID())
	}
	h.mu.Lock()
	idle := !h.running
	h.mu.Unlock()
	reply, err := h.cron.Command(line, h.now())
	if err != nil {
		return reply, err
	}
	if id := h.ID(); id != "" && h.cron.Live() > 0 {
		store, err := session.OpenIn(h.dir)
		if err == nil {
			err = h.recordHeader(store, id)
		}
		if err != nil {
			return reply, fmt.Errorf("the cron is kept, and tofu --continue does not find the session until its first turn: %w", err)
		}
	}
	if reply.CheckGoals && idle {
		h.fireAfterTurn()
	}
	h.armCron()
	return reply, nil
}

func (h *Host) cronState() CronState {
	state := CronState{Jobs: []CronJob{}}
	for _, job := range h.cron.Jobs() {
		spec := job.Spec()
		one := CronJob{ID: job.ID, Schedule: spec.Schedule, Prompt: spec.Prompt, Paused: spec.Paused, Ended: job.Ended}
		if !job.Next.IsZero() {
			one.Next = &job.Next
		}
		if job.Live() {
			state.Live++
		}
		if job.Live() && job.Noun() == "goal" {
			state.Goals++
		}
		state.Jobs = append(state.Jobs, one)
	}
	return state
}

func (h *Host) armCron() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ticking != nil || h.closed || h.joins {
		return
	}
	h.ticking = time.AfterFunc(konst.CronPollMillis*time.Millisecond, func() {
		h.mu.Lock()
		h.ticking = nil
		h.dropRead()
		h.mu.Unlock()
		_ = safe.Call(h.report, "cron", func() error {
			h.fire(h.cron.Due(context.Background(), h.now(), cron.Tick))
			return nil
		})
		h.armCron()
	})
}

func (h *Host) fire(fires []cron.Fire) {
	for _, fired := range fires {
		if fired.Prompt == "" {
			h.note(fired.Line)
			continue
		}
		origin := llm.Origin{Source: llm.SourceCronFire, ID: fired.ID}
		if job, err := h.cron.Job(fired.ID); err == nil {
			origin.Schedule = job.Spec().Schedule
		}
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		h.fired = append(h.fired, fired.ID)
		if !h.running {
			h.begin(Pick{Wire: h.pick.Wire, Model: h.pick.Model, Effort: h.pick.Effort, Fired: fired.ID}, fired.Prompt, origin)
			h.mu.Unlock()
			continue
		}
		h.unread = append(h.unread, fired)
		inbox := h.inbox
		h.mu.Unlock()
		h.emitter().emit(Event{Kind: EventNote, Text: fired.Line, Detail: fired.Prompt, Origin: wireOrigin(origin)})
		inbox.PostFired(fired.Prompt, origin)
		h.answerNobody()
	}
}

func (h *Host) answerNobody() {
	h.asks.mu.Lock()
	defer h.asks.mu.Unlock()
	for _, waiting := range h.asks.waiting {
		waiting.reply <- unattendedNow
	}
	h.asks.waiting = nil
}

func (h *Host) dropRead() {
	h.unread = slices.DeleteFunc(h.unread, func(fired cron.Fire) bool { return !h.inbox.Holds(fired.Prompt) })
}

func (h *Host) finishRead() {
	h.mu.Lock()
	h.dropRead()
	var read []string
	h.fired = slices.DeleteFunc(h.fired, func(id string) bool {
		answered := !slices.ContainsFunc(h.unread, func(fired cron.Fire) bool { return fired.ID == id })
		if answered {
			read = append(read, id)
		}
		return answered
	})
	answer := h.answer
	h.mu.Unlock()
	h.finish(read, answer)
}

func (h *Host) finish(fired []string, answer string) {
	for _, id := range fired {
		if note := h.cron.Finished(id, answer); note != "" {
			h.note(note)
		}
	}
}

func (h *Host) ended(out *emitter) {
	h.mu.Lock()
	stopped, fired, unread, answer := h.stopped, h.fired, h.unread, h.answer
	h.running, h.cancel, h.fired, h.unread = false, nil, nil, nil
	h.mu.Unlock()
	out.emit(Event{Kind: EventTurnEnded})
	h.finish(slices.DeleteFunc(fired, func(id string) bool {
		return slices.ContainsFunc(unread, func(again cron.Fire) bool { return again.ID == id })
	}), answer)
	h.fire(unread)
	if !stopped {
		h.fireAfterTurn()
	}
	h.armCron()
}

func (h *Host) fireAfterTurn() {
	h.fire(h.cron.Due(context.Background(), h.now(), cron.TurnEnded))
}
