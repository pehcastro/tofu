package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	statusFlags        = "usage: tofu login --status [--json] [--redact]"
	redactFlag         = "--redact"
	statusNoCredential = "no credential is stored"
	statusServing      = "serving"
	statusAttention    = "needs attention"
	statusInUse        = "in use"
	statusSetAside     = "set aside"
	statusExpired      = "expired"
	statusUnchosen     = "usable, not chosen"
	noAccountYet       = "no account captured yet"
	statusKeyNotStored = "not stored"
	statusKeyColumn    = 18
)

type accountReport struct {
	ID        int64          `json:"id"`
	Account   string         `json:"account"`
	Login     string         `json:"login"`
	Plan      string         `json:"plan"`
	State     string         `json:"state"`
	Attention bool           `json:"needs_attention"`
	Windows   []windowReport `json:"windows,omitempty"`
}

type sourceReport struct {
	Subscription string          `json:"subscription"`
	Accounts     []accountReport `json:"accounts"`
}

type statusReport struct {
	Headline   string         `json:"headline"`
	State      string         `json:"state"`
	Sources    []sourceReport `json:"sources"`
	Gate       string         `json:"jev_key"`
	Keys       []keyReport    `json:"keys,omitempty"`
	ReportedAt time.Time      `json:"reported_at"`
}

type keyReport struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

func storedKeyReports() ([]keyReport, error) {
	stored, err := sys.StoredKeys()
	if err != nil {
		return nil, err
	}
	names := sys.KeyNames()
	for _, name := range slices.Sorted(maps.Keys(stored)) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	reports := make([]keyReport, 0, len(names))
	for _, name := range names {
		state := statusKeyNotStored
		if value := stored[name]; value != "" {
			state = widget.Mask(value) + " in the credential store"
		}
		reports = append(reports, keyReport{Name: name, State: state})
	}
	return reports, nil
}

func keyLines(keys []keyReport) string {
	lines := []string{"", "keys"}
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s%-*s %s", reportIndent, statusKeyColumn, key.Name, key.State))
	}
	return strings.Join(lines, "\n") + "\n"
}

func statusVerb(args []string, out, errOut io.Writer, shade palette, now time.Time, urls map[quota.Provider]string) int {
	asJSON, redact := false, false
	for _, arg := range args {
		switch arg {
		case jsonFlag:
			asJSON = true
		case redactFlag:
			redact = true
		default:
			_, _ = fmt.Fprintln(errOut, statusFlags)
			return exitUsage
		}
	}
	report, err := credentialStatus(now, redact, urls)
	if err == nil && asJSON {
		err = writeJSON(out, report)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu login: %v\n", err)
		return exitVerdict
	}
	if !asJSON {
		_, _ = fmt.Fprint(out, statusText(report, shade, now, outputWidth(out))+keyLines(report.Keys))
	}
	return exitOK
}

func credentialStatus(now time.Time, redact bool, urls map[quota.Provider]string) (statusReport, error) {
	report := statusReport{
		Headline:   statusNoCredential,
		State:      statusAttention,
		Gate:       openRouterStatus(),
		ReportedAt: now,
	}
	keys, err := storedKeyReports()
	if err != nil {
		return report, err
	}
	report.Keys = keys
	store, err := openStoredCredentials()
	if err != nil || store == nil {
		return report, err
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return report, err
	}
	results, err := pollRows(context.Background(), store, rows, time.Now, urls)
	if err != nil {
		return report, err
	}
	report.Sources = statusSources(rows, results, redact, now)
	report.Headline, report.State = statusHeadline(report.Sources)
	return report, nil
}

func statusSources(rows []cred.Row, results []pollResult, redact bool, now time.Time) []sourceReport {
	var order []cred.Provider
	for _, row := range rows {
		if !slices.Contains(order, row.Credential.Provider) {
			order = append(order, row.Credential.Provider)
		}
	}
	sources := make([]sourceReport, 0, len(order))
	for _, provider := range order {
		source := sourceReport{Subscription: string(provider)}
		chosen, _ := quota.Pick(statusCandidates(provider, rows, results, now), quota.Provider(provider), now)
		for index, row := range rows {
			if row.Credential.Provider == provider {
				source.Accounts = append(source.Accounts, accountOf(row, results[index], row.ID != chosen.ID, redact, now))
			}
		}
		sources = append(sources, source)
	}
	return sources
}

func statusCandidates(provider cred.Provider, rows []cred.Row, results []pollResult, now time.Time) []quota.Candidate {
	candidates := make([]quota.Candidate, 0, len(rows))
	for index, row := range rows {
		if row.Credential.Provider != provider || row.Unusable(now) != "" {
			continue
		}
		candidates = append(candidates, quota.Candidate{
			ID:       row.ID,
			Provider: quota.Provider(provider),
			Report:   results[index].report,
		})
	}
	return candidates
}

func accountOf(row cred.Row, result pollResult, unchosen, redact bool, now time.Time) accountReport {
	state, attention := accountState(row, result, unchosen, now)
	return accountReport{
		ID:        row.ID,
		Account:   accountName(row.Credential.Identity, redact),
		Login:     strings.TrimPrefix(row.State(now), string(row.Credential.Provider)+" "),
		Plan:      accountPlan(row, result, now),
		State:     state,
		Attention: attention,
		Windows:   windowsOf(result.report),
	}
}

func accountName(identity cred.Identity, redact bool) string {
	if redact {
		return cmp.Or(maskedAccount(identity), noAccountYet)
	}
	return cmp.Or(identity.Email, identity.AccountID, noAccountYet)
}

func accountState(row cred.Row, result pollResult, unchosen bool, now time.Time) (string, bool) {
	condition := credentialState(result, now)
	switch {
	case row.DisabledCause != "":
		return statusSetAside, true
	case row.Unusable(now) != "":
		return statusExpired, true
	case condition != usageServingState:
		return condition, true
	case unchosen:
		return statusUnchosen, false
	}
	return statusInUse, false
}

func accountPlan(row cred.Row, result pollResult, now time.Time) string {
	if row.Unusable(now) != "" {
		return "not polled"
	}
	return cmp.Or(result.report.Plan, "not reported by "+string(row.Credential.Provider))
}

func statusHeadline(sources []sourceReport) (string, string) {
	total, attention := 0, 0
	for _, source := range sources {
		for _, account := range source.Accounts {
			total++
			if account.Attention {
				attention++
			}
		}
	}
	switch {
	case total == 0:
		return statusNoCredential, statusAttention
	case attention > 0:
		return fmt.Sprintf("%d of %s need attention", attention, accountCount(total)), statusAttention
	}
	return accountCount(total) + ", all serving", statusServing
}

func accountCount(total int) string {
	if total == 1 {
		return "1 account"
	}
	return strconv.Itoa(total) + " accounts"
}
