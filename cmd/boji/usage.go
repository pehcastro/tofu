package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"boji/internal/llm/cred"
	"boji/internal/llm/quota"
	"boji/internal/sys"
)

type pollResult struct {
	report quota.Report
	err    error
}

func usageVerb(args []string, out, errOut io.Writer) int {
	if len(args) != 0 {
		_, _ = fmt.Fprintln(errOut, "usage: boji usage")
		return exitUsage
	}
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji usage: %v\n", err)
		return exitVerdict
	}
	if len(results) == 0 {
		_, _ = fmt.Fprintln(out, "no subscription credential, run boji login anthropic or boji login codex")
		_, _ = fmt.Fprintln(out, quota.SpendLimitLine())
		return exitOK
	}
	now := time.Now()
	for _, result := range results {
		if result.err != nil {
			_, _ = fmt.Fprintln(out, quota.DoctorLine(result.report, result.err, now))
			continue
		}
		for _, line := range result.report.Lines(now) {
			_, _ = fmt.Fprintln(out, line)
		}
	}
	_, _ = fmt.Fprintln(out, quota.SpendLimitLine())
	return exitOK
}

func quotaDoctorLines(now time.Time) []string {
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return []string{"quota: unreadable: " + err.Error(), quota.SpendLimitLine()}
	}
	lines := make([]string, 0, len(results)+1)
	for _, result := range results {
		lines = append(lines, quota.DoctorLine(result.report, result.err, now))
	}
	return append(lines, quota.SpendLimitLine())
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
