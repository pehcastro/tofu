package main

import (
	"cmp"
	"context"
	"io"
	"os"
	"slices"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/jev"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/widget"
)

const (
	statusVerbName = "login --status"
	redactFlag     = "--redact"
	statusFlags    = "tofu login --status [--json] [--redact]"
	noAccountYet   = "no account captured yet"
)

type accountState string

const (
	stateInUse         accountState = "in_use"
	stateStandby       accountState = "standby"
	stateSetAside      accountState = "set_aside"
	stateRefreshFailed accountState = "refresh_failed"
	stateExpired       accountState = "expired"
	stateRefused       accountState = "refused"
	stateSpent         accountState = "spent"
	stateRateLimited   accountState = "rate_limited"
	stateUnread        accountState = "unread"
	stateUnchecked     accountState = "unchecked"
)

type statusData struct {
	Subscriptions []subscriptionStatus `json:"subscriptions"`
	Keys          []keyStatus          `json:"keys"`
}

type subscriptionStatus struct {
	Source   string          `json:"source"`
	Accounts []accountStatus `json:"accounts"`
}

type accountStatus struct {
	ID        int64          `json:"id"`
	Account   string         `json:"account"`
	State     accountState   `json:"state"`
	Login     string         `json:"login"`
	ReloginBy time.Time      `json:"relogin_by,omitzero"`
	Plan      string         `json:"plan,omitempty"`
	Windows   []windowStatus `json:"windows,omitempty"`
}

type windowStatus struct {
	ID       string    `json:"id"`
	Used     float64   `json:"used"`
	ResetsAt time.Time `json:"resets_at,omitzero"`
	Only     []string  `json:"only,omitempty"`
}

type keyStatus struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Variable string `json:"variable"`
	Key      string `json:"key,omitempty"`
	name     string
	use      string
	hint     string
}

func keyStatuses(resolve func(variable string) string) []keyStatus {
	keys := []keyStatus{
		{Role: "classifier", Provider: openRouterName, Variable: sys.OpenRouterKeyName, name: models.OpenRouter.Display(),
			use: "judges tool calls with jev-latest", hint: "tofu login " + openRouterName},
		{Role: "classifier", Provider: typeSafeName, Variable: sys.TypeSafeKeyName, name: models.TypeSafe.Display(),
			use: "used only without OpenRouter"},
		{Role: "web_search", Provider: braveName, Variable: sys.BraveSearchKeyName, name: braveDisplay,
			use: "backs the web search tool", hint: "tofu login " + braveName},
		{Role: "meta_models", Provider: metaName, Variable: sys.MetaMuseKeyName, name: models.Meta.Display(),
			use: "serves the meta models", hint: "tofu login " + metaName},
	}
	for i := range keys {
		if value := resolve(keys[i].Variable); value != "" {
			keys[i].Key = widget.Mask(value)
		}
	}
	return keys
}

func statusVerb(args []string, out, errOut io.Writer, now time.Time, urls map[quota.Provider]string) int {
	asJSON, redact := false, false
	for _, arg := range args {
		switch arg {
		case jsonFlag:
			asJSON = true
		case redactFlag:
			redact = true
		default:
			return loginFailed(statusVerbName, usageRefusal("unknown flag "+arg, statusFlags), asJSON, out, errOut, now)
		}
	}
	data, problems, err := credentialStatus(now, redact, urls)
	if err != nil {
		return loginFailed(statusVerbName, failure(err.Error(), "tofu doctor"), asJSON, out, errOut, now)
	}
	if asJSON {
		return jsonExit(out, cli.Envelope{Verb: statusVerbName, OK: true, At: now, Data: data, Problems: problems})
	}
	page := cli.Detect(out, os.Environ())
	if page.Print(out, statusLines(page, data, now)) != nil {
		return exitVerdict
	}
	return exitOK
}

func credentialStatus(now time.Time, redact bool, urls map[quota.Provider]string) (statusData, []cli.Problem, error) {
	data := statusData{Keys: keyStatuses(func(variable string) string {
		key, _ := jev.KeyFor(sys.CredentialFileName, variable)
		return key
	})}
	store, err := openStoredCredentials()
	if err != nil || store == nil {
		return data, nil, err
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return data, nil, err
	}
	results, err := pollRows(context.Background(), store, rows, time.Now, urls)
	if err != nil {
		return data, nil, err
	}
	library, libraryErr := modelLibrary("")
	var problems []cli.Problem
	if libraryErr != nil {
		problems = append(problems, cli.Problem{What: "the model library did not load, so scoped windows name no model", Hint: "tofu doctor"})
	}
	if len(results) > 0 && results[0].recordErr != nil {
		problems = append(problems, cli.Problem{What: quotaReadingNotWritten + results[0].recordErr.Error()})
	}
	var providers []cred.Provider
	for _, row := range rows {
		if !slices.Contains(providers, row.Credential.Provider) {
			providers = append(providers, row.Credential.Provider)
		}
	}
	for _, provider := range providers {
		chosen, _ := quota.Pick(statusCandidates(provider, rows, results, now), quota.Provider(provider), nil, now)
		source := subscriptionStatus{Source: string(provider)}
		for index, row := range rows {
			if row.Credential.Provider == provider {
				source.Accounts = append(source.Accounts, accountOf(row, results[index], row.ID == chosen.ID, redact, library, now))
			}
		}
		data.Subscriptions = append(data.Subscriptions, source)
	}
	return data, problems, nil
}

func statusCandidates(provider cred.Provider, rows []cred.Row, results []pollResult, now time.Time) []quota.Candidate {
	candidates := make([]quota.Candidate, 0, len(rows))
	for index, row := range rows {
		if row.Credential.Provider == provider && row.Unusable(now) == "" {
			candidates = append(candidates, quota.Candidate{ID: row.ID, Provider: quota.Provider(provider), Report: results[index].report})
		}
	}
	return candidates
}

func accountOf(row cred.Row, result pollResult, chosen, redact bool, library models.Library, now time.Time) accountStatus {
	account := accountStatus{
		ID:      row.ID,
		Account: accountName(row.Credential.Identity, redact),
		State:   stateOf(row, result, chosen, now),
		Login:   row.Credential.Kind,
		Plan:    result.report.Plan,
	}
	account.ReloginBy, _ = row.ReloginBy()
	for _, window := range result.report.Windows {
		if !window.Used.Reported {
			continue
		}
		shown := windowStatus{ID: window.ID, Used: window.Used.Fraction, ResetsAt: window.ResetsAt}
		for _, model := range library.Models {
			if model.Use != models.UseExcluded && !window.Binds(nil) && window.Binds(model.Windows) {
				shown.Only = append(shown.Only, model.ID)
			}
		}
		account.Windows = append(account.Windows, shown)
	}
	return account
}

func stateOf(row cred.Row, result pollResult, chosen bool, now time.Time) accountState {
	switch {
	case row.DisabledCause == setAsideCause:
		return stateSetAside
	case row.DisabledCause != "":
		return stateRefreshFailed
	case row.Unusable(now) != "":
		return stateExpired
	case result.unusable != "":
		return stateUnchecked
	}
	switch quota.Diagnose(result.report, result.err) {
	case quota.ConditionCredentialBroken:
		return stateRefused
	case quota.ConditionWindowSpent:
		return stateSpent
	case quota.ConditionUnknown:
		if transport.KindOf(result.err) == transport.KindRateLimit {
			return stateRateLimited
		}
		if result.err != nil {
			return stateUnread
		}
	case quota.ConditionServing:
	}
	if chosen {
		return stateInUse
	}
	return stateStandby
}

func accountName(identity cred.Identity, redact bool) string {
	if redact {
		return cmp.Or(maskedAccount(identity), noAccountYet)
	}
	return cmp.Or(identity.Email, identity.AccountID, noAccountYet)
}
