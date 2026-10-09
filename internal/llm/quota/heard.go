package quota

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	anthropicUnifiedPrefix = "anthropic-ratelimit-unified-"
	anthropicRejected      = "rejected"
	codexPrefix            = "x-codex-"
)

func headerNumber(header http.Header, name string) *float64 {
	number, err := strconv.ParseFloat(strings.TrimSpace(header.Get(name)), 64)
	if err != nil {
		return nil
	}
	return &number
}

func fromAnthropicHeaders(header http.Header, now time.Time) Report {
	report := Report{Provider: ClaudeSub, FetchedAt: now, Source: SourceHeaders}
	for _, unified := range []struct {
		id       string
		duration time.Duration
	}{{fiveHourWindow, fiveHours}, {sevenDayWindow, week}} {
		prefix := anthropicUnifiedPrefix + unified.id + "-"
		bucket := anthropicBucket{}
		if used := headerNumber(header, prefix+"utilization"); used != nil {
			percent := *used * percentFull
			if strings.TrimSpace(header.Get(prefix+"status")) == anthropicRejected {
				percent = max(percent, percentFull)
			}
			bucket.Utilization = &percent
		}
		if reset := headerNumber(header, prefix+"reset"); reset != nil {
			if at := epochTime(*reset); !at.IsZero() {
				bucket.ResetsAt = at.Format(time.RFC3339)
			}
		}
		report.appendBucket(unified.id, unified.duration, &bucket)
	}
	return report
}

func fromCodexHeaders(header http.Header, now time.Time) Report {
	report := Report{Provider: CodexSub, FetchedAt: now, Source: SourceHeaders}
	for _, name := range []string{"primary", "secondary"} {
		prefix := codexPrefix + name + "-"
		body := codexWindowBody{UsedPercent: headerNumber(header, prefix+"used-percent"), ResetAt: headerNumber(header, prefix+"reset-at")}
		if body.UsedPercent == nil {
			continue
		}
		if minutes := headerNumber(header, prefix+"window-minutes"); minutes != nil {
			seconds := *minutes * time.Minute.Seconds()
			body.LimitWindowSeconds = &seconds
		}
		if window, ok := codexWindow(name, &body, now); ok {
			report.Windows = append(report.Windows, window)
		}
	}
	return report
}

func (p *Poller) Heard(account Account, header http.Header) error {
	var heard Report
	switch account.Provider {
	case ClaudeSub:
		heard = fromAnthropicHeaders(header, p.now())
	case CodexSub:
		heard = fromCodexHeaders(header, p.now())
	default:
		panic("quota: unknown provider " + string(account.Provider))
	}
	shelf := p.shelf(account)
	if len(heard.Windows) == 0 || account.AccountID == "" || (p.now().Sub(shelf.read().HeardAt) < heardEvery && !heard.Exhausted()) {
		return nil
	}
	lock, err := shelf.try()
	if lock == nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	held := shelf.read()
	merged := held.Report
	merged.Provider, merged.FetchedAt, merged.Source = heard.Provider, heard.FetchedAt, heard.Source
	merged.Windows = slices.Concat(heard.Windows, slices.DeleteFunc(merged.Windows, func(old Window) bool {
		return slices.ContainsFunc(heard.Windows, func(window Window) bool { return window.ID == old.ID })
	}))
	held.Report, held.HeardAt, held.FreshUntil = merged, heard.FetchedAt, heard.FetchedAt.Add(p.freshFor(merged))
	if err := shelf.write(held); err != nil {
		return err
	}
	p.note(account, heard)
	return nil
}
