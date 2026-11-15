package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"tofu/internal/agent/cron"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/safe"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
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
		h.fireDue(cron.TurnEnded)
	}
	h.armCron()
	return reply, nil
}

func (h *Host) cronState() CronState {
	state := CronState{Jobs: []CronJob{}}
	for _, job := range h.cron.Jobs() {
		spec := job.Spec()
		one := CronJob{ID: job.ID, Schedule: spec.Schedule, Prompt: spec.Prompt, Paused: spec.Paused, Ended: job.Ended, LastResult: job.LastResult, LastFired: job.LastFired, Fires: job.Fires, Expires: spec.Expires, Until: spec.Until}
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
			h.fireDue(cron.Tick)
			return nil
		})
		h.armCron()
	})
}

func (h *Host) cronMoved() {
	h.bus.mu.Lock()
	h.fanOut(followLine{Cron: true})
	h.bus.mu.Unlock()
}

func (h *Host) fire(fires []cron.Fire) {
	if len(fires) > 0 {
		h.cronMoved()
	}
	for _, fired := range fires {
		if fired.Prompt == "" {
			h.note(fired.Line)
			continue
		}
		origin := core.Origin{Source: core.SourceCronFire, ID: fired.ID}
		if job, err := h.cron.Job(fired.ID); err == nil {
			origin.Schedule = job.Spec().Schedule
		}
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		if h.heldForFork(func() { h.fire([]cron.Fire{fired}) }) {
			h.mu.Unlock()
			continue
		}
		h.fired = append(h.fired, fired.ID)
		h.cron.Hand(fired.ID, h.now())
		if !h.running {
			h.begin(Pick{Wire: h.pick.Wire, Model: h.pick.Model, Effort: h.pick.Effort, Fired: fired.ID}, fired.Prompt, origin)
			h.mu.Unlock()
			continue
		}
		h.unread = append(h.unread, fired)
		inbox := h.inbox
		h.mu.Unlock()
		h.emitter().emit(Event{Kind: EventNote, Text: fired.Line, Detail: fired.Prompt, Origin: wireOrigin(origin)})
		inbox.Deliver(fired.Prompt, origin)
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

func (h *Host) finishRead(status Status) {
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
	h.finish(read, answer, status == StatusFinished)
}

func (h *Host) finish(fired []string, answer string, answered bool) {
	for _, id := range fired {
		h.mu.Lock()
		checked := h.cronOut[id]
		delete(h.cronOut, id)
		h.mu.Unlock()
		if job, err := h.cron.Job(id); err == nil {
			now := h.now()
			outsideMS := (checked + now.Sub(job.Handed)).Milliseconds()
			h.wall(walls.Wall{Op: walls.OpCron, Name: id, MS: now.Sub(job.LastFired).Milliseconds(), At: job.LastFired.UnixMilli(), OK: answered, OutsideMS: &outsideMS})
		}
		if note := h.cron.Finished(id, answer); note != "" {
			h.note(note)
		}
	}
	if len(fired) > 0 {
		h.cronMoved()
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
	}), answer, false)
	h.carrySteers(out)
	h.fire(unread)
	if !stopped {
		h.fireDue(cron.TurnEnded)
	}
	h.armCron()
}

func (h *Host) fireDue(moment cron.Moment) {
	checks, clock := outside.Open(context.Background(), time.Now)
	fires := h.cron.Due(checks, h.now(), moment)
	h.mu.Lock()
	for _, fired := range fires {
		if fired.Prompt != "" {
			h.cronOut[fired.ID] += clock.Outside()
		}
	}
	h.mu.Unlock()
	h.fire(fires)
}
