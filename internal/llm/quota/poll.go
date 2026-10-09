package quota

import (
	"cmp"
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"time"

	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

const (
	anthropicUsageURL      = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL          = "https://chatgpt.com/backend-api/wham/usage"
	ClaudeUsageURLVariable = "TOFU_CLAUDE_USAGE_URL"
	CodexUsageURLVariable  = "TOFU_CODEX_USAGE_URL"
)

const (
	pollTimeout       = 15 * time.Second
	freshFor          = 5 * time.Minute
	freshJitter       = 0.25
	coolFloor         = time.Minute
	coolCap           = 10 * time.Minute
	heardEvery        = time.Minute
	loggedLookback    = 7 * 24 * time.Hour
	lockRetry         = 25 * time.Millisecond
	nearLimitWatch    = 0.75
	nearLimitWatchFor = 2 * time.Minute
	nearLimitClose    = 0.90
	nearLimitCloseFor = time.Minute
	nearLimitAt       = 0.99
	nearLimitAtFor    = 30 * time.Second
)

type Credential interface {
	Access(ctx context.Context) (string, error)
}

type Account struct {
	Provider      Provider
	AccountID     string
	Row           int64
	Credential    Credential
	ClientVersion string
}

type Poller struct {
	http   *http.Client
	now    func() time.Time
	spread func() float64
	urls   map[Provider]string
	record func(Reading) error
	dir    string
}

func NewPoller(
	httpClient *http.Client,
	now func() time.Time,
	urls map[Provider]string,
	record func(Reading) error,
) (*Poller, error) {
	dir, err := sys.QuotaDir()
	if err != nil {
		return nil, err
	}
	if urls == nil {
		urls = map[Provider]string{
			ClaudeSub: cmp.Or(os.Getenv(ClaudeUsageURLVariable), anthropicUsageURL),
			CodexSub:  cmp.Or(os.Getenv(CodexUsageURLVariable), codexUsageURL),
		}
	}
	return &Poller{http: cmp.Or(httpClient, &http.Client{}), now: now, spread: rand.Float64, urls: urls, record: record, dir: dir}, nil
}

func (p *Poller) Poll(ctx context.Context, account Account) (Report, error) {
	if account.AccountID == "" {
		report, _, err := p.fetch(ctx, account)
		if err != nil {
			return Report{Provider: account.Provider, FetchedAt: p.now()}, err
		}
		p.note(account, report)
		return report, nil
	}
	shelf := p.shelf(account)
	if report, answered, err := p.answer(account, shelf.read()); answered {
		return report, err
	}
	lock, err := shelf.take(ctx)
	if err != nil {
		return p.lastGood(account, shelf.read()), transport.Fail("quota.Poll", transport.KindTimeout, err,
			"another tofu process is asking the %s usage endpoint for this account", account.Provider)
	}
	defer func() { _ = lock.Close() }()
	held := shelf.read()
	if report, answered, err := p.answer(account, held); answered {
		return report, err
	}
	report, wait, err := p.fetch(ctx, account)
	now := p.now()
	if err == nil {
		_ = shelf.write(shelved{Report: report, FreshUntil: now.Add(p.freshFor(report))})
		p.note(account, report)
		return report, nil
	}
	if ctx.Err() != nil || transport.KindOf(err) == transport.KindMissingCredential {
		return p.lastGood(account, held), err
	}
	held.Backoff = min(max(2*held.Backoff, coolFloor), coolCap)
	held.RetryAt = now.Add(cmp.Or(wait, held.Backoff))
	held.Kind, held.Status = transport.KindOf(err), statusOf(err)
	_ = shelf.write(held)
	return p.lastGood(account, held), err
}

func (p *Poller) answer(account Account, held shelved) (Report, bool, error) {
	now := p.now()
	if now.Before(held.FreshUntil) {
		return held.Report, true, nil
	}
	if !now.Before(held.RetryAt) {
		return Report{}, false, nil
	}
	return p.lastGood(account, held), true, &transport.Error{
		Kind:   held.Kind,
		Op:     "quota.Poll",
		Status: held.Status,
		Detail: "the " + string(account.Provider) + " usage endpoint answered " + strconv.Itoa(held.Status) +
			" and is not asked again before " + held.RetryAt.UTC().Format(time.RFC3339),
	}
}

func (p *Poller) lastGood(account Account, held shelved) Report {
	report := held.Report
	if report.FetchedAt.IsZero() {
		report = p.logged(account)
	}
	report.Stale = !report.FetchedAt.IsZero()
	if held.RetryAt.After(p.now()) {
		report.RetryAt = held.RetryAt
	}
	return report
}

func (p *Poller) logged(account Account) Report {
	readings, _, _ := ReadReadings(p.dir, p.now().Add(-loggedLookback))
	for i := len(readings) - 1; i >= 0; i-- {
		if reading := readings[i]; reading.Provider == account.Provider && reading.Account == account.Row {
			report := Report{Provider: account.Provider, FetchedAt: reading.At, Source: SourceLog}
			for _, window := range reading.Windows {
				report.Windows = append(report.Windows, Window{ID: window.ID, Used: Used{Fraction: window.Used, Reported: true}, ResetsAt: window.ResetsAt})
			}
			return report
		}
	}
	return Report{Provider: account.Provider}
}

func (p *Poller) freshFor(report Report) time.Duration {
	if report.Source == SourceEndpoint {
		fullest := 0.0
		for _, window := range report.binding(nil) {
			fullest = max(fullest, window.Used.Fraction)
		}
		switch {
		case fullest >= nearLimitAt:
			return nearLimitAtFor
		case fullest >= nearLimitClose:
			return nearLimitCloseFor
		case fullest >= nearLimitWatch:
			return nearLimitWatchFor
		}
	}
	return time.Duration(float64(freshFor) * (1 + freshJitter*(2*p.spread()-1)))
}

func (p *Poller) note(account Account, report Report) {
	if p.record != nil {
		_ = p.record(ReadingOf(account.Row, report))
	}
}

func (p *Poller) fetch(ctx context.Context, account Account) (Report, time.Duration, error) {
	token, err := account.Credential.Access(ctx)
	if err != nil {
		return Report{}, 0, transport.Fail("quota.Poll", transport.KindMissingCredential, err,
			"the %s credential could not be resolved", account.Provider)
	}
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.urls[account.Provider], nil)
	if err != nil {
		return Report{}, 0, transport.Fail("quota.Poll", transport.KindBadRequest, err, "building the usage request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	switch account.Provider {
	case ClaudeSub:
		request.Header.Set("User-Agent", anthropic.ClaudeCodeUserAgent(cmp.Or(account.ClientVersion, anthropic.PinnedClaudeCodeVersion)))
	case CodexSub:
		request.Header.Set("User-Agent", "tofu")
		if account.AccountID != "" {
			request.Header.Set("ChatGPT-Account-Id", account.AccountID)
		}
	default:
		panic("quota: unknown provider " + string(account.Provider))
	}
	response, err := p.http.Do(request)
	if err != nil {
		return Report{}, 0, transport.Fail("quota.Poll", transport.KindProvider, err, "asking the %s usage endpoint", account.Provider)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Report{}, 0, transport.Fail("quota.Poll", transport.KindProvider, err, "reading the %s usage answer", account.Provider)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Report{}, transport.RetryAfter(response.Header, p.now()), &transport.Error{
			Kind:   transport.StatusKind(response.StatusCode),
			Op:     "quota.Poll",
			Status: response.StatusCode,
			Detail: "the " + string(account.Provider) + " usage endpoint answered " + strconv.Itoa(response.StatusCode) +
				" and it is not asked again inside this call",
		}
	}
	if account.Provider == ClaudeSub {
		report, err := FromAnthropicUsage(body, p.now())
		return report, 0, err
	}
	report, err := FromCodexUsage(body, p.now())
	return report, 0, err
}
