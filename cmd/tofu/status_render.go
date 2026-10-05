package main

import (
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/widget"
)

const reloginDay = "2 Jan"

func (s accountState) look() (cli.Verdict, string) {
	switch s {
	case stateInUse:
		return cli.Verdict{Mark: cli.Active, Text: "in use"}, ""
	case stateStandby:
		return cli.Verdict{Mark: cli.Idle, Text: "standby"}, ""
	case stateUnchecked:
		return cli.Verdict{Mark: cli.Idle, Text: "not checked"}, ""
	case stateSpent:
		return cli.Verdict{Mark: cli.Warn, Text: "spent"}, ""
	case stateRateLimited:
		return cli.Verdict{Mark: cli.Warn, Text: "rate limited"}, ""
	case stateUnread:
		return cli.Verdict{Mark: cli.Warn, Text: "usage unread"}, ""
	case stateSetAside:
		return cli.Verdict{Mark: cli.Fail, Text: "set aside"}, "set aside by hand"
	case stateRefreshFailed:
		return cli.Verdict{Mark: cli.Fail, Text: "set aside"}, "refresh failed, sign in again"
	case stateExpired:
		return cli.Verdict{Mark: cli.Fail, Text: "expired"}, "login expired, sign in again"
	case stateRefused:
		return cli.Verdict{Mark: cli.Fail, Text: "signed out"}, "the token was refused, sign in again"
	}
	panic("tofu login: unknown account state " + string(s))
}

func statusLines(page cli.Page, data statusData, now time.Time) []string {
	signedIn, attention := 0, 0
	var body []string
	for _, sub := range data.Subscriptions {
		body = append(body, cli.Indent(page.Label(sub.Source))...)
		for _, account := range sub.Accounts {
			card, calm := accountCard(page, sub.Source, account, now)
			body = append(append(body, card...), "")
			signedIn++
			if !calm {
				attention++
			}
		}
	}
	verdict := cli.Verdict{Mark: cli.Idle, Text: "none signed in"}
	var facts []string
	switch {
	case attention == 1:
		verdict = cli.Verdict{Mark: cli.Warn, Text: "1 needs attention"}
	case attention > 1:
		verdict = cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(attention) + " need attention"}
	case signedIn > 0:
		verdict = cli.Verdict{Mark: cli.Done, Text: "all ready"}
	}
	if signedIn > 0 {
		facts = []string{strconv.Itoa(signedIn) + " signed in"}
	}
	lines := append(page.Title("Accounts", facts, verdict), "", page.Section("language model", cli.Verdict{}))
	lines = append(lines, body...)
	lines = append(lines, keyRows(page, data.Keys, roleLLM)...)
	for _, group := range []struct {
		title string
		of    role
	}{{"classifier · jev, required", roleClassifier}, {"search", roleSearch}} {
		lines = append(lines, "", page.Section(group.title, cli.Verdict{}))
		lines = append(lines, keyRows(page, data.Keys, group.of)...)
	}
	return lines
}

func keyRows(page cli.Page, keys []keyStatus, of role) []string {
	var rows []cli.Row
	for _, key := range keys {
		if key.Role != of {
			continue
		}
		row := cli.Row{Mark: cli.Done, Cells: []string{key.name, key.Key}, Detail: key.use}
		if key.Key == "" {
			row.Mark, row.Cells[1], row.Hint = cli.Idle, "not set", key.hint
		}
		rows = append(rows, row)
	}
	return cli.Indent(page.Rows(rows)...)
}

func accountCard(page cli.Page, source string, account accountStatus, now time.Time) ([]string, bool) {
	verdict, reason := account.State.look()
	id := strconv.FormatInt(account.ID, 10)
	login, hint := account.Login, ""
	switch {
	case account.State == stateSetAside:
		login, hint = reason, "tofu login --enable "+id
	case reason != "":
		login, hint = reason, loginHint(source)
	case !account.ReloginBy.IsZero():
		login += " · re-login by " + account.ReloginBy.UTC().Format(reloginDay)
	}
	facts := []cli.Fact{{Label: "login", Text: login}, {Label: "plan", Text: account.Plan}}
	for _, window := range account.Windows {
		text := page.Bar(window.Used)
		switch {
		case len(window.Only) > 0:
			text += cli.Gap + page.Label("only "+strings.Join(window.Only, ", "))
		case !window.ResetsAt.IsZero():
			text += cli.Gap + page.Label("resets in "+widget.Until(window.ResetsAt.Sub(now)))
		}
		facts = append(facts, cli.Fact{Label: window.ID, Text: text})
	}
	lines := page.Facts(facts)
	if hint != "" {
		lines = append(lines, page.Hint(hint))
	}
	head := page.Label("#"+id) + cli.Gap + page.Subject(account.Account)
	return page.Card(head, verdict, lines), verdict.Mark != cli.Warn && verdict.Mark != cli.Fail
}
