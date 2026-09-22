package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	shipped "tofu/catalog"
	"tofu/internal/konst"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	"tofu/internal/widget"
)

const (
	statusFlags         = "usage: tofu login --status [--json] [--redact]"
	redactFlag          = "--redact"
	statusNoCredential  = "no credential is stored"
	statusServing       = "serving"
	statusAttention     = "needs attention"
	statusInUse         = "in use"
	statusSetAside      = "set aside"
	statusExpired       = "expired"
	statusUnchosen      = "usable, not chosen"
	noAccountYet        = "no account captured yet"
	accountNumberColumn = 6
	accountColumn       = 26
	accountFactIndent   = len(reportIndent) + accountNumberColumn
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
	Refusal      string          `json:"refusal,omitempty"`
	Fix          string          `json:"fix,omitempty"`
}

type statusReport struct {
	Headline   string         `json:"headline"`
	State      string         `json:"state"`
	Sources    []sourceReport `json:"sources"`
	Gate       string         `json:"jev_key"`
	ReportedAt time.Time      `json:"reported_at"`
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
		_, _ = fmt.Fprint(out, statusText(report, shade, now))
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
	report.Sources = statusSources(store, rows, results, redact, now)
	report.Headline, report.State = statusHeadline(report.Sources)
	return report, nil
}

func statusSources(store *cred.Store, rows []cred.Row, results []pollResult, redact bool, now time.Time) []sourceReport {
	slugs := subscriptionSlugs()
	var order []cred.Provider
	for _, row := range rows {
		if !slices.Contains(order, row.Credential.Provider) {
			order = append(order, row.Credential.Provider)
		}
	}
	sources := make([]sourceReport, 0, len(order))
	for _, provider := range order {
		source := sourceReport{Subscription: cmp.Or(slugs[string(provider)], string(provider))}
		var refusal cred.TwoAccounts
		_, _, err := store.RowAt(provider, now)
		refuses := errors.As(err, &refusal)
		if refuses {
			source.Refusal = fmt.Sprintf(
				"%d %s accounts are usable and nothing says which, so every turn refuses",
				len(refusal.IDs), provider)
			source.Fix = "tofu login --disable " + strconv.FormatInt(refusal.IDs[0], 10)
		}
		for index, row := range rows {
			if row.Credential.Provider == provider {
				source.Accounts = append(source.Accounts, accountOf(row, results[index], refuses, redact, now))
			}
		}
		sources = append(sources, source)
	}
	return sources
}

func subscriptionSlugs() map[string]string {
	slugs := make(map[string]string)
	layers, err := models.Layers(shipped.Files())
	if err != nil {
		return slugs
	}
	catalog, _ := models.Load(layers)
	for _, spec := range catalog.Subscriptions {
		slugs[spec.Wire] = string(spec.ID) + "-sub"
	}
	return slugs
}

func accountOf(row cred.Row, result pollResult, refuses, redact bool, now time.Time) accountReport {
	state, attention := accountState(row, result, refuses, now)
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

func accountState(row cred.Row, result pollResult, refuses bool, now time.Time) (string, bool) {
	condition := credentialState(result, now)
	switch {
	case row.DisabledCause != "":
		return statusSetAside, true
	case row.Unusable(now) != "":
		return statusExpired, true
	case refuses:
		return statusUnchosen, true
	case condition != usageServingState:
		return condition, true
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
	total, attention, refusing := 0, 0, ""
	for _, source := range sources {
		if source.Refusal != "" && refusing == "" {
			refusing = source.Subscription
		}
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
	case refusing != "":
		return refusing + " refuses every turn", statusAttention
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

func statusText(report statusReport, shade palette, now time.Time) string {
	painted := shade.settled(report.State)
	if report.State != statusServing {
		painted = shade.unsettled(report.State)
	}
	lines := []string{headline(report.Headline, painted, len(report.State))}
	for _, source := range report.Sources {
		lines = append(lines, "", source.Subscription)
		for _, account := range source.Accounts {
			lines = append(lines, accountLines(account, shade, now)...)
		}
		if source.Refusal != "" {
			lines = append(lines, blockerLines("refuses", source.Refusal, source.Fix)...)
		}
	}
	lines = append(lines, "")
	lines = append(lines, wrapped(jevName, report.Gate)...)
	return strings.Join(lines, "\n") + "\n"
}

func accountLines(account accountReport, shade palette, now time.Time) []string {
	state := shade.settled(account.State)
	if account.Attention {
		state = shade.unsettled(account.State)
	}
	lines := []string{reportIndent +
		widget.Pad("#"+strconv.FormatInt(account.ID, 10), accountNumberColumn) +
		widget.Pad(account.Account, accountColumn) + state}
	lines = append(lines, factLines("login", account.Login)...)
	lines = append(lines, factLines("plan", account.Plan)...)
	reported := false
	for _, window := range account.Windows {
		if !window.Reported {
			continue
		}
		reported = true
		lines = append(lines, strings.Repeat(" ", accountFactIndent)+window.text(shade, now))
	}
	if !reported {
		lines = append(lines, factLines("windows", usageNoWindowReported)...)
	}
	return lines
}

func factLines(label, text string) []string {
	var lines []string
	for _, line := range widget.Wrap(text, konst.ReportWidthChars-accountFactIndent-usageWindowColumn) {
		lines = append(lines, strings.Repeat(" ", accountFactIndent)+widget.Pad(label, usageWindowColumn)+line)
		label = ""
	}
	return lines
}
