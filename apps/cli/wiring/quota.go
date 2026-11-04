package wiring

import (
	"context"
	"strconv"
	"sync"
	"time"

	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/quota"
	"tofu/internal/models/transport"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/ui/tui/frame/frame"
)

const (
	UsageServingState      = "serving"
	Unreadable             = "unreadable: "
	QuotaReadingNotWritten = "the quota reading was not recorded: "
	noVendorInATest        = "a test may not reach a vendor, so pass pollRows a stub url"
)

type PollResult struct {
	Row       int64
	Name      string
	Report    quota.Report
	Err       error
	Unusable  string
	RecordErr error
}

func WindowPercent(w host.WindowReport) string {
	if !w.Reported {
		return "use not reported"
	}
	return render.Percent(w.Used)
}

func CredentialReports(results []PollResult, now time.Time) []host.CredentialReport {
	reports := make([]host.CredentialReport, 0, len(results))
	for _, result := range results {
		reports = append(reports, host.CredentialReport{
			Provider: string(result.Report.Provider),
			Account:  QuotaAccount(result.Row),
			Name:     result.Name,
			Plan:     result.Report.Plan,
			State:    credentialState(result, now),
			Windows:  windowsOf(result.Report),
			ReadAt:   result.Report.FetchedAt.UTC(),
			Source:   string(result.Report.Source),
			Stale:    result.Report.Stale,
			RetryAt:  result.Report.RetryAt.UTC(),
		})
	}
	return reports
}

func QuotaAccount(row int64) string { return "#" + strconv.FormatInt(row, 10) }

func windowsOf(report quota.Report) []host.WindowReport {
	windows := make([]host.WindowReport, 0, len(report.Windows))
	for _, window := range report.Windows {
		windows = append(windows, host.WindowReport{
			ID:       window.ID,
			Used:     window.Used.Fraction,
			Reported: window.Used.Reported,
			ResetsAt: window.ResetsAt,
		})
	}
	return windows
}

func credentialState(result PollResult, now time.Time) string {
	state := quotaState(result, now)
	if result.RecordErr == nil {
		return state
	}
	return state + ", " + QuotaReadingNotWritten + result.RecordErr.Error()
}

func quotaState(result PollResult, now time.Time) string {
	if result.Unusable != "" {
		return "not polled, " + result.Unusable
	}
	switch quota.Diagnose(result.Report, result.Err) {
	case quota.ConditionServing:
		return UsageServingState
	case quota.ConditionWindowSpent:
		if until, waits := result.Report.WaitUntil(now); waits {
			return "every window is spent, back at " + until.UTC().Format(time.RFC3339)
		}
		return "a window is spent and no reset time was reported, so waiting is not authorized"
	case quota.ConditionCredentialBroken:
		return "the credential is broken, run " + LoginHint(string(result.Report.Provider))
	case quota.ConditionUnknown:
		switch {
		case transport.KindOf(result.Err) == transport.KindRateLimit:
			return "signed in, the usage endpoint rate limited this check"
		case result.Err != nil:
			return Unreadable + result.Err.Error()
		}
		return "signed in, no window reported"
	}
	panic("tofu usage: unknown quota condition")
}

func PollCredentials(ctx context.Context, now func() time.Time, kept *quota.Poller) ([]PollResult, error) {
	path, err := accounts.Path()
	if err != nil {
		return nil, err
	}
	present, err := fs.Exists(path)
	if err != nil || !present {
		return nil, err
	}
	store, err := accounts.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	return pollStore(ctx, store, now, kept)
}

func pollStore(ctx context.Context, store *accounts.Store, now func() time.Time, kept *quota.Poller) ([]PollResult, error) {
	rows, err := store.List()
	if err != nil {
		return nil, err
	}
	if kept != nil {
		return PollRowsOn(ctx, store, rows, now, nil, kept)
	}
	return PollRows(ctx, store, rows, now, nil)
}

func PollRows(ctx context.Context, store *accounts.Store, rows []accounts.Row, now func() time.Time, urls map[quota.Provider]string) ([]PollResult, error) {
	readings, recordErr := fs.QuotaDir()
	var recording sync.Mutex
	record := func(reading quota.Reading) error {
		recording.Lock()
		defer recording.Unlock()
		if recordErr != nil {
			return recordErr
		}
		recordErr = quota.AppendReading(readings, reading)
		return recordErr
	}
	poller, err := quota.NewPoller(nil, now, urls, record)
	if err != nil {
		return nil, err
	}
	results, err := PollRowsOn(ctx, store, rows, now, urls, poller)
	for index := range results {
		results[index].RecordErr = recordErr
	}
	return results, err
}

func PollRowsOn(ctx context.Context, store *accounts.Store, rows []accounts.Row, now func() time.Time, urls map[quota.Provider]string, poller *quota.Poller) ([]PollResult, error) {
	results := make([]PollResult, len(rows))
	versions, _ := SubFingerprint(".")
	var polling sync.WaitGroup
	defer polling.Wait()
	for i, row := range rows {
		provider := quota.Provider(row.Credential.Provider)
		results[i] = PollResult{Row: row.ID, Name: AccountName(row.Credential.Identity, false), Report: quota.Report{Provider: provider}}
		if urls == nil && credential.CredentialsHiddenFromTests() {
			results[i].Unusable = noVendorInATest
			continue
		}
		if results[i].Unusable = row.Unusable(now()); results[i].Unusable != "" {
			continue
		}
		spec, err := accounts.Lookup(string(row.Credential.Provider))
		if err != nil {
			return nil, err
		}
		polling.Go(func() {
			results[i].Err = project.Crashed(".", safe.Call(nil, "quota poll", func() (err error) {
				results[i].Report, err = poller.Poll(ctx, quota.Account{
					Provider:      provider,
					AccountID:     row.Credential.Identity.AccountID,
					Row:           row.ID,
					Credential:    accounts.NewAccountManager(store, spec, row.ID),
					ClientVersion: ClientVersion(versions, row.Credential.Provider),
				})
				return err
			}))
		})
	}
	return results, nil
}

func RecordQuotaReading(reading quota.Reading) error {
	readings, err := fs.QuotaDir()
	if err != nil {
		return err
	}
	return quota.AppendReading(readings, reading)
}

func (h HeldStore) Quota() []frame.Quota {
	if h.Err != nil {
		return nil
	}
	return QuotaFrames(pollStore(context.Background(), h.Store, time.Now, nil))
}

func QuotaFrames(results []PollResult, err error) []frame.Quota {
	if err != nil {
		return nil
	}
	var quotas []frame.Quota
	for _, result := range results {
		for _, window := range result.Report.Windows {
			quotas = append(quotas, frame.Quota{
				Label:    string(result.Report.Provider) + " " + window.ID,
				Account:  QuotaAccount(result.Row),
				Fraction: window.Used.Fraction,
				Reported: window.Used.Reported,
				ResetsAt: window.ResetsAt,
				ReadAt:   result.Report.FetchedAt,
				Stale:    result.Report.Stale,
			})
		}
	}
	return quotas
}
