package status

import (
	"context"
	"io"
	"os"
	"slices"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/quota"
	"tofu/internal/models/transport"
)

const (
	statusVerbName = "login --status"
	redactFlag     = "--redact"
	statusFlags    = "tofu login --status [--json] [--redact]"
)

func LoginVerb(args []string, out, errOut io.Writer, now time.Time, urls map[quota.Provider]string) int {
	asJSON, redact := false, false
	for _, arg := range args {
		switch arg {
		case output.JSONFlag:
			asJSON = true
		case redactFlag:
			redact = true
		default:
			return output.Refused(statusVerbName, output.UsageRefusal("unknown flag "+arg, statusFlags), asJSON, out, errOut, now)
		}
	}
	data, problems, err := credentialStatus(now, redact, urls)
	if err != nil {
		return output.Refused(statusVerbName, output.Failure(err.Error(), "tofu doctor"), asJSON, out, errOut, now)
	}
	if asJSON {
		return output.JSONExit(out, cli.Envelope{Verb: statusVerbName, OK: true, At: now, Data: data, Problems: problems})
	}
	page := cli.Detect(out, os.Environ())
	if page.Print(out, LoginLines(page, data, now)) != nil {
		return output.ExitVerdict
	}
	return output.ExitOK
}

func credentialStatus(now time.Time, redact bool, urls map[quota.Provider]string) (host.Accounts, []cli.Problem, error) {
	data := host.Accounts{Keys: wiring.KeyStatuses(wiring.ResolvedKey)}
	polled, err := withSpend(context.Background(), data.Keys, now)
	defer polled()
	if err != nil {
		return data, nil, err
	}
	store, err := wiring.OpenStoredCredentials()
	if err != nil || store == nil {
		return data, nil, err
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return data, nil, err
	}
	results, err := wiring.PollRows(context.Background(), store, rows, time.Now, urls)
	if err != nil {
		return data, nil, err
	}
	library, libraryErr := wiring.ModelLibrary("")
	var problems []cli.Problem
	if libraryErr != nil {
		problems = append(problems, cli.Problem{What: "the model library did not load, so scoped windows name no model", Hint: "tofu doctor"})
	}
	if len(results) > 0 && results[0].RecordErr != nil {
		problems = append(problems, cli.Problem{What: wiring.QuotaReadingNotWritten + results[0].RecordErr.Error()})
	}
	var providers []accounts.Provider
	for _, row := range rows {
		if !slices.Contains(providers, row.Credential.Provider) {
			providers = append(providers, row.Credential.Provider)
		}
	}
	for _, provider := range providers {
		chosen, _ := quota.Pick(statusCandidates(provider, rows, results, now), quota.Provider(provider), nil, now)
		source := host.SubscriptionStatus{Role: host.RoleLLM, Source: string(provider)}
		for index, row := range rows {
			if row.Credential.Provider != provider {
				continue
			}
			attempts, err := store.Attempts(row.ID)
			if err != nil {
				return data, nil, err
			}
			source.Accounts = append(source.Accounts, accountOf(row, results[index], attempts, row.ID == chosen.ID, redact, library, now))
		}
		data.Subscriptions = append(data.Subscriptions, source)
	}
	return data, problems, nil
}

func statusCandidates(provider accounts.Provider, rows []accounts.Row, results []wiring.PollResult, now time.Time) []quota.Candidate {
	candidates := make([]quota.Candidate, 0, len(rows))
	for index, row := range rows {
		if row.Credential.Provider == provider && row.Unusable(now) == "" {
			candidates = append(candidates, quota.Candidate{ID: row.ID, Provider: quota.Provider(provider), Report: results[index].Report, LastUsed: row.LastUsed})
		}
	}
	return candidates
}

func accountOf(row accounts.Row, result wiring.PollResult, attempts []accounts.Attempt, chosen, redact bool, library catalog.Library, now time.Time) host.AccountStatus {
	refreshed := "not refreshed yet"
	if !row.Credential.Refreshed.IsZero() {
		refreshed = "refreshed " + render.Until(now.Sub(row.Credential.Refreshed)) + " ago"
	}
	account := host.AccountStatus{
		ID:      row.ID,
		Account: wiring.AccountName(row.Credential.Identity, redact),
		State:   stateOf(row, result, chosen, now),
		Login:   row.Credential.Kind + " · " + refreshed + ", kept alive while tofu runs",
		Plan:    result.Report.Plan,
	}
	for _, attempt := range attempts {
		account.Attempts = append(account.Attempts, host.RefreshTry{At: attempt.At.UTC(), Status: attempt.Status, Answer: attempt.Answer})
	}
	if account.State == host.AccountRefreshFailed {
		account.Login = "refresh failed, sign in again"
	}
	if lastRefreshFailed(account) && (account.State == host.AccountRefreshFailed || account.State == host.AccountRefused) {
		last := attempts[len(attempts)-1]
		account.Login = "refresh failed " + render.Until(now.Sub(last.At)) + " ago · " + last.Said()
	}
	account.ReloginBy, _ = row.ReloginBy(now)
	for _, window := range result.Report.Windows {
		if !window.Used.Reported {
			continue
		}
		shown := host.WindowStatus{ID: window.ID, Used: window.Used.Fraction, ResetsAt: window.ResetsAt}
		for _, model := range library.Models {
			if model.Use != catalog.UseExcluded && !window.Binds(nil) && window.Binds(model.Windows) {
				shown.Only = append(shown.Only, model.ID)
			}
		}
		account.Windows = append(account.Windows, shown)
	}
	return account
}

func stateOf(row accounts.Row, result wiring.PollResult, chosen bool, now time.Time) host.AccountState {
	switch {
	case row.DisabledCause == wiring.SetAsideCause:
		return host.AccountSetAside
	case row.DisabledCause != "":
		return host.AccountRefreshFailed
	case row.Unusable(now) != "":
		return host.AccountExpired
	case result.Unusable != "":
		return host.AccountUnchecked
	}
	switch quota.Diagnose(result.Report, result.Err) {
	case quota.ConditionCredentialBroken:
		return host.AccountRefused
	case quota.ConditionWindowSpent:
		return host.AccountSpent
	case quota.ConditionUnknown:
		if transport.KindOf(result.Err) == transport.KindRateLimit {
			return host.AccountRateLimited
		}
		if result.Err != nil {
			return host.AccountUnread
		}
	case quota.ConditionServing:
	}
	if chosen {
		return host.AccountInUse
	}
	return host.AccountStandby
}
