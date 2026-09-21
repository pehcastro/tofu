package turn

import (
	"context"
	"net/http/httptrace"
	"strconv"
	"sync"

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

func askCountingAttempts(ctx context.Context, model Model, request llm.Request) (llm.Decision, int, error) {
	var counted requestsPerHost
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GetConn: counted.sending})
	decision, err := model.Ask(traced, request)
	sent := counted.attempt()
	if decision.Attempts > llm.AttemptsUnreported && int(decision.Attempts) != sent {
		decision.Warnings = append(decision.Warnings,
			"requests this process put on the wire: "+strconv.Itoa(sent)+
				", and the wire answered on attempt "+decision.Attempts.String())
	}
	return decision, sent, err
}
