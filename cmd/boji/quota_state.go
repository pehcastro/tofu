package main

import (
	"context"
	"time"

	"boji/internal/llm/cred"
	"boji/internal/llm/quota"
	"boji/internal/sys"
	"boji/internal/transport"
	"boji/internal/widget"
)

const (
	usageWindowColumn = 10
	usageServingState = "serving"
)

type pollResult struct {
	report quota.Report
	err    error
}

type windowReport struct {
	ID       string    `json:"id"`
	Used     float64   `json:"used_fraction"`
	Reported bool      `json:"used_reported"`
	ResetsAt time.Time `json:"resets_at,omitzero"`
}

func (w windowReport) percent() string {
	if !w.Reported {
		return "use not reported"
	}
	return widget.Percent(w.Used)
}

func (w windowReport) text(shade palette, now time.Time) string {
	return widget.Pad(w.ID, usageWindowColumn) + shade.full(w.Used, widget.Quota(w.Used, w.ResetsAt, now))
}

type credentialReport struct {
	Provider string         `json:"provider"`
	Plan     string         `json:"plan,omitempty"`
	State    string         `json:"state"`
	Windows  []windowReport `json:"windows,omitempty"`
}

func credentialReports(results []pollResult, now time.Time) []credentialReport {
	reports := make([]credentialReport, 0, len(results))
	for _, result := range results {
		windows := make([]windowReport, 0, len(result.report.Windows))
		for _, window := range result.report.Windows {
			windows = append(windows, windowReport{
				ID:       window.ID,
				Used:     window.Used.Fraction,
				Reported: window.Used.Reported,
				ResetsAt: window.ResetsAt,
			})
		}
		reports = append(reports, credentialReport{
			Provider: string(result.report.Provider),
			Plan:     result.report.Plan,
			State:    credentialState(result, now),
			Windows:  windows,
		})
	}
	return reports
}

func credentialState(result pollResult, now time.Time) string {
	switch quota.Diagnose(result.report, result.err) {
	case quota.ConditionServing:
		return usageServingState
	case quota.ConditionWindowSpent:
		if until, waits := result.report.WaitUntil(now); waits {
			return "every window is spent, back at " + until.UTC().Format(time.RFC3339)
		}
		return "a window is spent and no reset time was reported, so waiting is not authorized"
	case quota.ConditionCredentialBroken:
		return "the credential is broken, run boji login " + string(result.report.Provider)
	case quota.ConditionUnknown:
		switch {
		case transport.KindOf(result.err) == transport.KindRateLimit:
			return "signed in, the usage endpoint rate limited this check"
		case result.err != nil:
			return doctorUnreadable + result.err.Error()
		}
		return "signed in, no window reported"
	}
	panic("boji usage: unknown quota condition")
}

func pollCredentials(ctx context.Context, now func() time.Time) ([]pollResult, error) {
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
	poller, err := quota.NewPoller(nil, now, nil)
	if err != nil {
		return nil, err
	}
	results := make([]pollResult, 0, len(rows))
	for _, row := range rows {
		spec, err := cred.Lookup(string(row.Credential.Provider))
		if err != nil {
			return nil, err
		}
		report, err := poller.Poll(ctx, quota.Account{
			Provider:   quota.Provider(row.Credential.Provider),
			AccountID:  row.Credential.Identity.AccountID,
			Credential: cred.NewManager(store, spec),
		})
		results = append(results, pollResult{report: report, err: err})
	}
	return results, nil
}
