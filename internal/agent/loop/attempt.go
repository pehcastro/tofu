package loop

import (
	"context"
	"net/http/httptrace"
	"strconv"
	"sync"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/platform/outside"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
)

type requestsPerHost struct {
	mu       sync.Mutex
	answered string
	sent     map[string]int
}

func (r *requestsPerHost) sending(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sent == nil {
		r.sent = map[string]int{}
	}
	r.sent[host]++
	r.answered = host
}

func (r *requestsPerHost) attempt() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return max(r.sent[r.answered], session.FirstAttempt)
}

type clocked struct {
	ctx     context.Context
	clock   *outside.Clock
	started time.Time
}

func clockFrom(ctx context.Context) clocked {
	ctx, clock := outside.Open(ctx, time.Now)
	return clocked{ctx: ctx, clock: clock, started: time.Now()}
}

func clockedWall(op walls.Op, name string, since clocked, ok bool) walls.Wall {
	outsideMS := since.clock.Outside().Milliseconds()
	return walls.Wall{Op: op, Name: name, MS: time.Since(since.started).Milliseconds(), At: since.started.UnixMilli(), OK: ok, OutsideMS: &outsideMS}
}

func (r *record) wall(at session.Event, wall walls.Wall) {
	at.Kind = session.EventWall
	r.add(at, wall)
}

type requestTiming struct {
	attempt    int
	started    time.Time
	durationMS int64
	outsideMS  int64
}

func askCountingAttempts(ctx context.Context, model Model, request core.Request) (core.Decision, requestTiming, error) {
	var counted requestsPerHost
	asking := clockFrom(ctx)
	decision, err := model.Ask(httptrace.WithClientTrace(asking.ctx, &httptrace.ClientTrace{GetConn: counted.sending}), request)
	sent := requestTiming{attempt: counted.attempt(), started: asking.started, durationMS: time.Since(asking.started).Milliseconds(), outsideMS: asking.clock.Outside().Milliseconds()}
	if decision.Attempts > core.AttemptsUnreported && int(decision.Attempts) != sent.attempt {
		decision.Warnings = append(decision.Warnings,
			"requests this process put on the wire: "+strconv.Itoa(sent.attempt)+
				", and the wire answered on attempt "+decision.Attempts.String())
	}
	return decision, sent, err
}

func (r *record) requested(id string, op walls.Op, name string, timing requestTiming, decision core.Decision, err error) {
	at, ok := session.Event{Request: id}, err == nil
	r.wall(at, walls.Wall{Op: op, Name: name, MS: timing.durationMS, At: timing.started.UnixMilli(), OK: ok, OutsideMS: &timing.outsideMS})
	if op != walls.OpRequest {
		return
	}
	for _, waited := range []walls.Wall{{Op: walls.OpFirstToken, MS: decision.FirstTokenMS}, {Op: walls.OpLastByte, MS: decision.LastByteMS}} {
		if waited.MS > 0 {
			waited.Name, waited.At, waited.OK, waited.OutsideMS = name, timing.started.UnixMilli(), ok, &waited.MS
			r.wall(at, waited)
		}
	}
}
