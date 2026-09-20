package quota

import (
	"context"
	"errors"
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
	Credential Credential
}

type Poller struct {
	client   *transport.Client
	now      func() time.Time
	interval time.Duration
	urls     map[Provider]string
	mu       sync.Mutex
	last     map[Provider]time.Time
	cached   map[Provider]Report
}

func NewPoller(httpClient *http.Client, now func() time.Time, urls map[Provider]string) (*Poller, error) {
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
		urls = map[Provider]string{Anthropic: anthropicUsageURL, Codex: codexUsageURL}
	}
	return &Poller{
		client:   client,
		now:      now,
		interval: pollMinInterval,
		urls:     urls,
		last:     make(map[Provider]time.Time, len(urls)),
		cached:   make(map[Provider]Report, len(urls)),
	}, nil
}

func (p *Poller) Poll(ctx context.Context, account Account) (Report, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if last, seen := p.last[account.Provider]; seen && p.now().Sub(last) < p.interval {
		return p.cached[account.Provider], nil
	}
	report, err := p.fetch(ctx, account)
	if err != nil {
		return Report{Provider: account.Provider, FetchedAt: p.now()}, err
	}
	p.last[account.Provider] = p.now()
	p.cached[account.Provider] = report
	return report, nil
}

func (p *Poller) fetch(ctx context.Context, account Account) (Report, error) {
	token, err := account.Credential.Access(ctx)
	if err != nil {
		return Report{}, transport.Fail("quota.Poll", transport.KindMissingCredential, nil,
			"the %s credential could not be resolved", account.Provider)
	}
	request := transport.Request{Method: http.MethodGet, URL: p.urls[account.Provider], Header: http.Header{}}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	switch account.Provider {
	case Anthropic:
		request.Header.Set("User-Agent", claudeUserAgent)
	case Codex:
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
		status := 0
		if errors.As(err, &failure) {
			status = failure.Status
		}
		return Report{}, transport.Fail("quota.Poll", transport.KindOf(err), nil,
			"the %s usage endpoint answered %d and it is not polled again inside this call",
			account.Provider, status)
	}
	if account.Provider == Anthropic {
		return FromAnthropicUsage(response.Body, p.now())
	}
	return FromCodexUsage(response.Body, p.now())
}
