package main

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/cli/render"
	"tofu/internal/host"
)

const (
	reloginDay = "2 Jan"
	cardInset  = "  │ "
)

func stateLook(s accountState) (cli.Verdict, string) {
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
		return cli.Verdict{Mark: cli.Fail, Text: "set aside"}, ""
	case stateExpired:
		return cli.Verdict{Mark: cli.Fail, Text: "expired"}, "login expired, sign in again"
	case stateRefused:
		return cli.Verdict{Mark: cli.Fail, Text: "signed out"}, "the token was refused, sign in again"
	}
	panic("tofu login: unknown account state " + string(s))
}

func statusLines(page cli.Page, data statusData, now time.Time) []string {
	signedIn, attention := 0, 0
	var blocks [][]string
	for _, sub := range data.Subscriptions {
		label := cli.Indent(page.Label(sub.Source))
		for _, account := range sub.Accounts {
			card, calm := accountCard(page, sub.Source, account, now)
			blocks = append(blocks, append(label, card...))
			label = nil
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
	lines = append(lines, separated(append(blocks, keyBlocks(page, data.Keys, roleLLM, now)...))...)
	for _, group := range []struct {
		title string
		of    role
	}{{"classifier · jev, required", roleClassifier}, {"search", roleSearch}} {
		lines = append(lines, "", page.Section(group.title, cli.Verdict{}))
		lines = append(lines, separated(keyBlocks(page, data.Keys, group.of, now))...)
	}
	return lines
}

func separated(blocks [][]string) []string {
	var lines []string
	for index, block := range blocks {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, block...)
	}
	return lines
}

func cardFacts(page cli.Page, facts []cli.Fact) []string {
	lines := page.Facts(facts)
	for index := range lines {
		lines[index] = widget.Fit(lines[index], page.Width-widget.Cells(cardInset))
	}
	return lines
}

func keyBlocks(page cli.Page, keys []keyStatus, of role, now time.Time) [][]string {
	var blocks [][]string
	var unset []cli.Row
	for _, key := range keys {
		if key.Role != of {
			continue
		}
		facts := keyFactsOf(key.Role, key.Provider)
		if key.Key == "" {
			unset = append(unset, cli.Row{Mark: cli.Idle, Cells: []string{facts.name, "not set"}, Hint: facts.hint()})
			continue
		}
		head := page.Subject(facts.name) + cli.Gap + page.Label(key.Key)
		body := cardFacts(page, []cli.Fact{{Label: "use", Text: facts.use}, {Label: "calls", Text: callsText(key, now)}, standingFact(key, now)})
		blocks = append(blocks, page.Card(head, cli.Verdict{Mark: cli.Done, Text: "set"}, body))
	}
	if len(unset) > 0 {
		blocks = append(blocks, cli.Indent(page.Rows(unset)...))
	}
	return blocks
}

func callsText(key keyStatus, now time.Time) string {
	if key.Calls == 0 {
		return "none yet"
	}
	text := plural(key.Calls, "call") + " · last " + widget.Until(now.Sub(key.LastUsedAt)) + " ago"
	all := key.Periods[len(key.Periods)-1].KeySpend
	switch all.CostSource {
	case host.CostProvider:
		text += " · " + usdText(all.CostUSD)
	case host.CostList:
		text += " · " + usdText(all.CostUSD) + " est."
	case host.CostNone:
	}
	return text
}

func standingFact(key keyStatus, now time.Time) cli.Fact {
	if failure := key.LastError; failure != nil && failure.At.After(key.LastUsedAt) {
		return cli.Fact{Label: "error", Text: strconv.Itoa(failure.Status) + " " + failure.What + " · " + widget.Until(now.Sub(failure.At)) + " ago"}
	}
	var standing []string
	if limit := key.Limit; limit != nil {
		switch {
		case limit.Capped && limit.LimitUSD != nil && limit.RemainingUSD != nil:
			standing = append(standing, usdText(*limit.RemainingUSD)+" of "+usdText(*limit.LimitUSD)+" left")
		case limit.ProviderSpend != nil:
			standing = append(standing, "no cap · "+usdText(limit.ProviderSpend.MonthUSD)+" this month")
		}
	}
	if balance := key.Balance; balance != nil {
		standing = append(standing, "balance "+usdText(balance.BalanceUSD))
	}
	for _, allowance := range key.Allowances {
		text := strconv.Itoa(allowance.Remaining) + " of " + strconv.Itoa(allowance.Limit) + " " + string(allowance.Unit) + " left"
		if !allowance.ResetsAt.IsZero() {
			text += ", resets in " + widget.Until(allowance.ResetsAt.Sub(now))
		}
		standing = append(standing, text)
	}
	return cli.Fact{Label: "limit", Text: strings.Join(standing, " · ")}
}

func usdText(usd float64) string {
	return strconv.FormatFloat(usd, 'f', min(max(2, significantDecimals(usd)), 7), 64) + " USD"
}

func significantDecimals(usd float64) int {
	decimals := 0
	for magnitude := math.Abs(usd); magnitude > 0 && magnitude < 1; magnitude *= 10 {
		decimals++
	}
	return decimals + 1
}

func lastRefreshFailed(account accountStatus) bool {
	tries := account.Attempts
	return len(tries) > 0 && tries[len(tries)-1].Status != http.StatusOK
}

func accountCard(page cli.Page, source string, account accountStatus, now time.Time) ([]string, bool) {
	verdict, reason := stateLook(account.State)
	id := strconv.FormatInt(account.ID, 10)
	login, hint := account.Login, ""
	switch {
	case account.State == stateSetAside:
		login, hint = reason, "tofu login --enable "+id
	case account.State == stateRefreshFailed, account.State == stateRefused && lastRefreshFailed(account):
		hint = loginHint(source)
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
	lines := cardFacts(page, facts)
	if hint != "" {
		lines = append(lines, page.Hint(hint))
	}
	head := page.Label("#"+id) + cli.Gap + page.Subject(account.Account)
	return page.Card(head, verdict, lines), verdict.Mark != cli.Warn && verdict.Mark != cli.Fail
}
