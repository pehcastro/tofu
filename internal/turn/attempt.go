package turn

import (
	"context"
	"net/http/httptrace"
	"strconv"
	"sync"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
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

type requestTiming struct {
	attempt    int
	durationMS int64
}

func askCountingAttempts(ctx context.Context, model Model, request llm.Request) (llm.Decision, requestTiming, error) {
	var counted requestsPerHost
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GetConn: counted.sending})
	started := time.Now()
	decision, err := model.Ask(traced, request)
	sent := requestTiming{attempt: counted.attempt(), durationMS: time.Since(started).Milliseconds()}
	if decision.Attempts > llm.AttemptsUnreported && int(decision.Attempts) != sent.attempt {
		decision.Warnings = append(decision.Warnings,
			"requests this process put on the wire: "+strconv.Itoa(sent.attempt)+
				", and the wire answered on attempt "+decision.Attempts.String())
	}
	return decision, sent, err
}
