package main

import (
	"context"
	"strconv"
	"time"

	"tofu/internal/host"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/widget"
)

const (
	usageServingState = "serving"
	noVendorInATest   = "a test may not reach a vendor, so pass pollRows a stub url"

	quotaReadingNotWritten = "the quota reading was not recorded: "
)

type pollResult struct {
	row       int64
	name      string
	report    quota.Report
	err       error
	unusable  string
	recordErr error
}

type (
	windowReport     = host.WindowReport
	credentialReport = host.CredentialReport
)

func windowPercent(w windowReport) string {
	if !w.Reported {
		return "use not reported"
	}
	return widget.Percent(w.Used)
}

func credentialReports(results []pollResult, now time.Time) []credentialReport {
	reports := make([]credentialReport, 0, len(results))
	for _, result := range results {
		reports = append(reports, credentialReport{
			Provider: string(result.report.Provider),
			Account:  quotaAccount(result.row),
			Name:     result.name,
			Plan:     result.report.Plan,
			State:    credentialState(result, now),
			Windows:  windowsOf(result.report),
			ReadAt:   result.report.FetchedAt.UTC(),
			Source:   string(result.report.Source),
			Stale:    result.report.Stale,
			RetryAt:  result.report.RetryAt.UTC(),
		})
	}
	return reports
}

func quotaAccount(row int64) string { return "#" + strconv.FormatInt(row, 10) }

func windowsOf(report quota.Report) []windowReport {
	windows := make([]windowReport, 0, len(report.Windows))
	for _, window := range report.Windows {
		windows = append(windows, windowReport{
			ID:       window.ID,
			Used:     window.Used.Fraction,
			Reported: window.Used.Reported,
			ResetsAt: window.ResetsAt,
		})
	}
	return windows
}

func credentialState(result pollResult, now time.Time) string {
	state := quotaState(result, now)
	if result.recordErr == nil {
		return state
	}
	return state + ", " + quotaReadingNotWritten + result.recordErr.Error()
}

func quotaState(result pollResult, now time.Time) string {
	if result.unusable != "" {
		return "not polled, " + result.unusable
	}
	switch quota.Diagnose(result.report, result.err) {
	case quota.ConditionServing:
		return usageServingState
	case quota.ConditionWindowSpent:
		if until, waits := result.report.WaitUntil(now); waits {
			return "every window is spent, back at " + until.UTC().Format(time.RFC3339)
		}
		return "a window is spent and no reset time was reported, so waiting is not authorized"
	case quota.ConditionCredentialBroken:
		return "the credential is broken, run " + loginHint(string(result.report.Provider))
	case quota.ConditionUnknown:
		switch {
		case transport.KindOf(result.err) == transport.KindRateLimit:
			return "signed in, the usage endpoint rate limited this check"
		case result.err != nil:
			return doctorUnreadable + result.err.Error()
		}
		return "signed in, no window reported"
	}
	panic("tofu usage: unknown quota condition")
}

func pollCredentials(ctx context.Context, now func() time.Time, kept *quota.Poller) ([]pollResult, error) {
	path, err := cred.Path()
	if err != nil {
		return nil, err
	}
	present, err := sys.Exists(path)
	if err != nil || !present {
		return nil, err
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return nil, err
	}
	if kept != nil {
		return pollRowsOn(ctx, store, rows, now, nil, kept)
	}
	return pollRows(ctx, store, rows, now, nil)
}

func pollRows(
	ctx context.Context,
	store *cred.Store,
	rows []cred.Row,
	now func() time.Time,
	urls map[quota.Provider]string,
) ([]pollResult, error) {
	readings, recordErr := quotaReadingDir()
	record := func(reading quota.Reading) error {
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
	results, err := pollRowsOn(ctx, store, rows, now, urls, poller)
	for index := range results {
		results[index].recordErr = recordErr
	}
	return results, err
}

func pollRowsOn(
	ctx context.Context,
	store *cred.Store,
	rows []cred.Row,
	now func() time.Time,
	urls map[quota.Provider]string,
	poller *quota.Poller,
) ([]pollResult, error) {
	results := make([]pollResult, 0, len(rows))
	versions, _ := subFingerprint(".")
	for _, row := range rows {
		provider, name := quota.Provider(row.Credential.Provider), accountName(row.Credential.Identity, false)
		if urls == nil && sys.CredentialsHiddenFromTests() {
			results = append(results, pollResult{row: row.ID, name: name, report: quota.Report{Provider: provider}, unusable: noVendorInATest})
			continue
		}
		if cause := row.Unusable(now()); cause != "" {
			results = append(results, pollResult{row: row.ID, name: name, report: quota.Report{Provider: provider}, unusable: cause})
			continue
		}
		spec, err := cred.Lookup(string(row.Credential.Provider))
		if err != nil {
			return nil, err
		}
		report, err := poller.Poll(ctx, quota.Account{
			Provider:      provider,
			AccountID:     row.Credential.Identity.AccountID,
			Row:           row.ID,
			Credential:    cred.NewAccountManager(store, spec, row.ID),
			ClientVersion: clientVersion(versions, row.Credential.Provider),
		})
		results = append(results, pollResult{row: row.ID, name: name, report: report, err: err})
	}
	return results, nil
}

func quotaReadingDir() (string, error) { return sys.QuotaDir() }

func recordQuotaReading(reading quota.Reading) error {
	readings, err := quotaReadingDir()
	if err != nil {
		return err
	}
	return quota.AppendReading(readings, reading)
}
