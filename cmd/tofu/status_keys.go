package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

const (
	openRouterAPI         = "https://openrouter.ai/api/v1"
	openRouterAPIVariable = "TOFU_OPENROUTER_API_URL"
	openRouterKeyPath     = "/key"
	openRouterCreditsPath = "/credits"
	openRouterKeyPrefix   = "sk-or-"
	rateLimitHeaders      = "X-RateLimit headers on the last call"
	balanceNeedsManager   = "the account balance needs a management key"
	outOfCredit           = "account out of credit"
	day                   = 24 * time.Hour
)

func braveReports() string {
	return "Brave reports monthly calls remaining and the reset; it has no spend API, so cost is estimated from " +
		strconv.FormatFloat(konst.BraveSearchDollarsPerCall*1000, 'f', -1, 64) + " USD per 1000 calls"
}

func withSpend(ctx context.Context, keys []keyStatus, now time.Time) (func(), error) {
	uses, err := ledger.KeyUses()
	if err != nil {
		return func() {}, err
	}
	for _, use := range uses {
		if at := slices.IndexFunc(keys, func(key keyStatus) bool { return key.Variable == use.Key }); at >= 0 {
			spendOn(&keys[at], use, now)
		}
	}
	var polls sync.WaitGroup
	for index := range keys {
		if key := &keys[index]; key.Provider == openRouterName && key.Key != "" {
			polls.Go(func() { limitOnOpenRouter(ctx, key, now) })
			polls.Go(func() { balanceOnOpenRouter(ctx, key, now) })
		}
	}
	return polls.Wait, nil
}

func spendOn(key *keyStatus, use ledger.KeyUse, now time.Time) {
	for _, read := range use.Allowances {
		allowance := host.KeyAllowance{From: rateLimitHeaders, Scope: host.AllowanceScope(read.Scope), Unit: host.AllowanceUnit(read.Unit),
			Limit: read.Limit, Remaining: read.Remaining, ResetsAt: read.ResetsAt, ReadAt: use.At}
		at := slices.IndexFunc(key.Allowances, func(held host.KeyAllowance) bool { return held.Scope == allowance.Scope && held.Unit == allowance.Unit })
		switch {
		case at < 0:
			key.Allowances = append(key.Allowances, allowance)
		case !use.At.Before(key.Allowances[at].ReadAt):
			key.Allowances[at] = allowance
		}
	}
	if use.Failed != nil {
		if key.LastError == nil || use.At.After(key.LastError.At) {
			what := use.Failed.What
			if use.Failed.Status == http.StatusPaymentRequired {
				what = outOfCredit
			}
			key.LastError = &host.KeyFailure{Status: use.Failed.Status, What: what, At: use.At}
		}
		return
	}
	if key.Periods == nil {
		key.Periods = []host.KeyPeriod{{Period: host.PeriodToday}, {Period: host.PeriodWeek}, {Period: host.PeriodMonth}, {Period: host.PeriodAll}}
	}
	key.Calls++
	key.Queries += use.Queries
	if use.At.After(key.LastUsedAt) {
		key.LastUsedAt = use.At
	}
	for index, start := range []time.Time{now.UTC().Truncate(day), now.Add(-7 * day), now.Add(-30 * day), {}} {
		if !use.At.Before(start) {
			key.Periods[index].KeySpend = addSpend(key.Periods[index].KeySpend, use)
		}
	}
	if use.Model == "" {
		return
	}
	at := slices.IndexFunc(key.ByModel, func(model host.KeyModelUse) bool { return model.Model == use.Model })
	if at < 0 {
		key.ByModel, at = append(key.ByModel, host.KeyModelUse{Model: use.Model}), len(key.ByModel)
	}
	key.ByModel[at].KeySpend = addSpend(key.ByModel[at].KeySpend, use)
}

func addSpend(spend host.KeySpend, use ledger.KeyUse) host.KeySpend {
	spend.Calls++
	spend.Tokens.InputTokens += use.Usage.InputTokens
	spend.Tokens.OutputTokens += use.Usage.OutputTokens
	spend.Tokens.CacheReadInputTokens += use.Usage.CacheReadInputTokens
	spend.Tokens.CacheWriteInputTokens += use.Usage.CacheWriteInputTokens
	spend.Tokens.ReasoningOutputTokens += use.Usage.ReasoningOutputTokens
	spend.UpstreamUSD += use.UpstreamUSD
	spend.CostSource = cmp.Or(spend.CostSource, host.CostNone)
	if use.Charge.USD > 0 {
		spend.CostUSD += use.Charge.USD
		spend.CostSource = host.CostSource(use.Charge.Source)
	}
	return spend
}

func readOpenRouter(ctx context.Context, variable, path string, into any) error {
	client, err := transport.New(transport.Config{AttemptTimeout: konst.KeyPollTimeoutMillis * time.Millisecond, Concurrency: 1})
	if err != nil {
		return err
	}
	response, err := client.Do(ctx, transport.Request{
		Method: http.MethodGet,
		URL:    cmp.Or(strings.TrimSpace(os.Getenv(openRouterAPIVariable)), openRouterAPI) + path,
		Header: http.Header{"Authorization": {"Bearer " + resolvedKey(variable)}, "Accept": {"application/json"}},
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(response.Body, into)
}

func limitOnOpenRouter(ctx context.Context, key *keyStatus, now time.Time) {
	var read struct {
		Data struct {
			Label     string   `json:"label"`
			Limit     *float64 `json:"limit"`
			Remaining *float64 `json:"limit_remaining"`
			Reset     string   `json:"limit_reset"`
			Usage     float64  `json:"usage"`
			Daily     float64  `json:"usage_daily"`
			Weekly    float64  `json:"usage_weekly"`
			Monthly   float64  `json:"usage_monthly"`
			BYOK      float64  `json:"byok_usage"`
			FreeTier  bool     `json:"is_free_tier"`
		} `json:"data"`
	}
	if err := readOpenRouter(ctx, key.Variable, openRouterKeyPath, &read); err != nil {
		key.LimitUnread = "openrouter GET /api/v1/key: " + err.Error()
		return
	}
	data := read.Data
	if !strings.HasPrefix(data.Label, openRouterKeyPrefix) {
		key.Label = data.Label
	}
	key.Limit = &host.KeyLimit{
		From: "openrouter GET /api/v1/key", Capped: data.Limit != nil, LimitUSD: data.Limit, RemainingUSD: data.Remaining, Resets: data.Reset,
		ProviderSpend: &host.ProviderSpend{TodayUSD: data.Daily, WeekUSD: data.Weekly, MonthUSD: data.Monthly, AllUSD: data.Usage, BYOKUSD: data.BYOK},
		FreeTier:      data.FreeTier, ReadAt: now,
	}
}

func balanceOnOpenRouter(ctx context.Context, key *keyStatus, now time.Time) {
	var read struct {
		Data struct {
			Credits float64 `json:"total_credits"`
			Used    float64 `json:"total_usage"`
		} `json:"data"`
	}
	err := readOpenRouter(ctx, key.Variable, openRouterCreditsPath, &read)
	var refused *transport.Error
	switch {
	case errors.As(err, &refused) && (refused.Status == http.StatusUnauthorized || refused.Status == http.StatusForbidden):
		key.BalanceUnread = balanceNeedsManager
	case err != nil:
		key.BalanceUnread = "openrouter GET /api/v1/credits: " + err.Error()
	default:
		key.Balance = &host.KeyBalance{From: "openrouter GET /api/v1/credits", CreditsUSD: read.Data.Credits, UsedUSD: read.Data.Used, BalanceUSD: read.Data.Credits - read.Data.Used, ReadAt: now}
	}
}
