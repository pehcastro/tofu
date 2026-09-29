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
		body = append(body, "", page.Section(sub.Source, cli.Verdict{}))
		for i, account := range sub.Accounts {
			if i > 0 {
				body = append(body, "")
			}
			card, calm := accountCard(page, sub.Source, account, now)
			body = append(body, card...)
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
	rows := make([]cli.Row, len(data.Keys))
	for i, key := range data.Keys {
		rows[i] = cli.Row{Mark: cli.Done, Cells: []string{spoken(key.Role), key.name, key.Key}, Detail: key.use}
		if key.Key == "" {
			rows[i].Mark, rows[i].Cells[2], rows[i].Hint = cli.Idle, "not set", key.hint
		}
	}
	lines := append(page.Title("Accounts", facts, verdict), body...)
	lines = append(lines, "", page.Section("keys", cli.Verdict{}))
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func accountCard(page cli.Page, source string, account accountStatus, now time.Time) ([]string, bool) {
	verdict, reason := account.State.look()
	id := strconv.FormatInt(account.ID, 10)
	login, hint := account.Login, ""
	switch {
	case account.State == stateSetAside:
		login, hint = reason, "tofu login --enable "+id
	case reason != "":
		login, hint = reason, "tofu login "+source
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
