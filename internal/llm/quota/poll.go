package quota

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"tofu/internal/transport"
)

const (
	anthropicUsageURL = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL     = "https://chatgpt.com/backend-api/wham/usage"
	claudeUserAgent   = "claude-cli/2.1.257 (external, cli)"
	pollTimeout       = 15 * time.Second
	pollMinInterval   = 5 * time.Minute
)

type Credential interface {
	Access(ctx context.Context) (string, error)
}

type Account struct {
	Provider   Provider
	AccountID  string
	Row        int64
	Credential Credential
}

type Poller struct {
	client   *transport.Client
	now      func() time.Time
	interval time.Duration
	urls     map[Provider]string
	record   func(Reading) error
	mu       sync.Mutex
	last     map[pollKey]time.Time
	cached   map[pollKey]Report
}

type pollKey struct {
	provider  Provider
	accountID string
}

func NewPoller(
	httpClient *http.Client,
	now func() time.Time,
	urls map[Provider]string,
	record func(Reading) error,
) (*Poller, error) {
	client, err := transport.New(transport.Config{
		AttemptTimeout: pollTimeout,
		Retries:        0,
		Concurrency:    1,
		HTTP:           httpClient,
		Now:            now,
	})
	if err != nil {
		return nil, err
	}
	if urls == nil {
		urls = map[Provider]string{ClaudeSub: anthropicUsageURL, CodexSub: codexUsageURL}
	}
	return &Poller{
		client:   client,
		now:      now,
		interval: pollMinInterval,
		urls:     urls,
		record:   record,
		last:     make(map[pollKey]time.Time, len(urls)),
		cached:   make(map[pollKey]Report, len(urls)),
	}, nil
}

func (p *Poller) Poll(ctx context.Context, account Account) (Report, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cacheable := account.AccountID != ""
	key := pollKey{provider: account.Provider, accountID: account.AccountID}
	if last, seen := p.last[key]; seen && cacheable && p.now().Sub(last) < p.interval {
		return p.cached[key], nil
	}
	report, err := p.fetch(ctx, account)
	if err != nil {
		return Report{Provider: account.Provider, FetchedAt: p.now()}, err
	}
	if cacheable {
		p.last[key] = p.now()
		p.cached[key] = report
	}
	if p.record != nil {
		_ = p.record(ReadingOf(account.Row, report))
	}
	return report, nil
}

func (p *Poller) fetch(ctx context.Context, account Account) (Report, error) {
	token, err := account.Credential.Access(ctx)
	if err != nil {
		return Report{}, transport.Fail("quota.Poll", transport.KindMissingCredential, err,
			"the %s credential could not be resolved", account.Provider)
	}
	request := transport.Request{Method: http.MethodGet, URL: p.urls[account.Provider], Header: http.Header{}}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	switch account.Provider {
	case ClaudeSub:
		request.Header.Set("User-Agent", claudeUserAgent)
	case CodexSub:
		request.Header.Set("User-Agent", "tofu")
		if account.AccountID != "" {
			request.Header.Set("ChatGPT-Account-Id", account.AccountID)
		}
	default:
		panic("quota: unknown provider " + string(account.Provider))
	}

	response, err := p.client.Do(ctx, request)
	if err != nil {
		var failure *transport.Error
		if !errors.As(err, &failure) {
			return Report{}, err
		}
		return Report{}, &transport.Error{
			Kind:      failure.Kind,
			Op:        "quota.Poll",
			Status:    failure.Status,
			RequestID: failure.RequestID,
			Detail: fmt.Sprintf("the %s usage endpoint answered %d and it is not polled again inside this call",
				account.Provider, failure.Status),
			Err: err,
		}
	}
	if account.Provider == ClaudeSub {
		return FromAnthropicUsage(response.Body, p.now())
	}
	return FromCodexUsage(response.Body, p.now())
}
