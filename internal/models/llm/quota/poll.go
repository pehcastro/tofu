package quota

import (
	"cmp"
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/transport"
	"tofu/internal/platform/fs"
)

const (
	pollTimeout       = konst.QuotaPollTimeoutSeconds * time.Second
	freshFor          = konst.QuotaFreshMinutes * time.Minute
	coolFloor         = konst.QuotaCoolFloorMinutes * time.Minute
	coolCap           = konst.QuotaCoolCapMinutes * time.Minute
	heardEvery        = konst.QuotaHeardEverySeconds * time.Second
	loggedLookback    = konst.QuotaLoggedLookbackDays * 24 * time.Hour
	lockRetry         = konst.QuotaLockRetryMillis * time.Millisecond
	nearLimitWatchFor = konst.QuotaNearLimitWatchSeconds * time.Second
	nearLimitCloseFor = konst.QuotaNearLimitCloseSeconds * time.Second
	nearLimitAtFor    = konst.QuotaNearLimitAtSeconds * time.Second
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

type Endpoint struct {
	URL         string
	SetHeaders  func(request *http.Request, account Account)
	Parse       func(body []byte, now time.Time) (Report, error)
	FromHeaders func(header http.Header, now time.Time) Report
}

type Endpoints map[Provider]Endpoint

func (e Endpoints) of(provider Provider) Endpoint {
	endpoint, known := e[provider]
	if !known {
		panic("quota: unknown provider " + string(provider))
	}
	return endpoint
}

type Poller struct {
	http      *http.Client
	now       func() time.Time
	spread    func() float64
	endpoints Endpoints
	record    func(Reading) error
	dir       string
}

func NewPoller(
	httpClient *http.Client,
	now func() time.Time,
	endpoints Endpoints,
	record func(Reading) error,
) (*Poller, error) {
	dir, err := fs.QuotaDir()
	if err != nil {
		return nil, err
	}
	return &Poller{http: cmp.Or(httpClient, &http.Client{}), now: now, spread: rand.Float64, endpoints: endpoints, record: record, dir: dir}, nil
}

func (p *Poller) Poll(ctx context.Context, account Account) (Report, error) {
	if account.AccountID == "" {
		report, _, err := p.fetch(ctx, account)
		if err != nil {
			return Report{Provider: account.Provider}, err
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
	defer func() { _ = fs.Unlock(lock) }()
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
		case fullest >= konst.QuotaNearLimitAt:
			return nearLimitAtFor
		case fullest >= konst.QuotaNearLimitClose:
			return nearLimitCloseFor
		case fullest >= konst.QuotaNearLimitWatch:
			return nearLimitWatchFor
		}
	}
	return Jittered(freshFor, p.spread())
}

func Jittered(base time.Duration, spread float64) time.Duration {
	return time.Duration(float64(base) * (1 + konst.QuotaJitter*(2*spread-1)))
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
	endpoint := p.endpoints.of(account.Provider)
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		return Report{}, 0, transport.Fail("quota.Poll", transport.KindBadRequest, err, "building the usage request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	endpoint.SetHeaders(request, account)
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
	report, err := endpoint.Parse(body, p.now())
	return report, 0, err
}
