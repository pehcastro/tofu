package main

import (
	"math"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/widget"
)

const reloginDay = "2 Jan"

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
	lines = append(lines, keyRows(page, data.Keys, roleLLM, now)...)
	for _, group := range []struct {
		title string
		of    role
	}{{"classifier · jev, required", roleClassifier}, {"search", roleSearch}} {
		lines = append(lines, "", page.Section(group.title, cli.Verdict{}))
		lines = append(lines, keyRows(page, data.Keys, group.of, now)...)
	}
	return lines
}

func keyRows(page cli.Page, keys []keyStatus, of role, now time.Time) []string {
	var rows []cli.Row
	var shown []keyStatus
	for _, key := range keys {
		if key.Role != of {
			continue
		}
		facts := keyFactsOf(key.Role, key.Provider)
		row := cli.Row{Mark: cli.Done, Cells: []string{facts.name, key.Key}, Detail: facts.use}
		if key.Key == "" {
			row.Mark, row.Cells[1], row.Hint = cli.Idle, "not set", facts.hint()
		}
		rows, shown = append(rows, row), append(shown, key)
	}
	var lines []string
	for index, line := range page.Rows(rows) {
		lines = append(lines, line)
		lines = append(lines, cli.Indent(cli.Indent(page.Facts(keyLines(page, shown[index], now))...)...)...)
	}
	return cli.Indent(lines...)
}

func recordedLines(page cli.Page, key keyStatus, now time.Time) []cli.Fact {
	if key.Calls == 0 {
		if key.Key == "" {
			return nil
		}
		return []cli.Fact{{Label: "calls", Text: page.Label("no calls yet")}}
	}
	calls := plural(key.Calls, "call") + " · last used " + widget.Until(now.Sub(key.LastUsedAt)) + " ago"
	if key.Queries > 0 {
		calls += " · queries " + strconv.Itoa(key.Queries)
	}
	facts := []cli.Fact{{Label: "calls", Text: calls}}
	for from := 0; from < len(key.Periods); {
		to := from
		for to+1 < len(key.Periods) && key.Periods[to+1].KeySpend == key.Periods[from].KeySpend {
			to++
		}
		label := string(key.Periods[from].Period)
		if to > from {
			label += " to " + string(key.Periods[to].Period)
		}
		if key.Periods[from].Calls > 0 {
			facts = append(facts, cli.Fact{Label: label, Text: spendText(key.Periods[from].KeySpend)})
		}
		from = to + 1
	}
	var answered []string
	for _, model := range key.ByModel {
		answered = append(answered, model.Model)
		if len(key.ByModel) > 1 {
			facts = append(facts, cli.Fact{Label: "on " + model.Model, Text: spendText(model.KeySpend)})
		}
	}
	return append(facts, cli.Fact{Label: "answered by", Text: strings.Join(answered, ", ")})
}

func keyLines(page cli.Page, key keyStatus, now time.Time) []cli.Fact {
	facts := recordedLines(page, key, now)
	if limit := key.Limit; limit != nil {
		text := "none"
		if limit.Capped && limit.LimitUSD != nil && limit.RemainingUSD != nil {
			text = usdText(*limit.RemainingUSD) + " of " + usdText(*limit.LimitUSD) + " left"
		}
		if spent := limit.ProviderSpend; spent != nil {
			text += " · spent today " + usdText(spent.TodayUSD) + ", week " + usdText(spent.WeekUSD) + ", month " + usdText(spent.MonthUSD) + ", all " + usdText(spent.AllUSD)
		}
		facts = append(facts, cli.Fact{Label: "key limit", Text: text})
	}
	facts = append(facts, cli.Fact{Label: "key limit", Text: key.LimitUnread})
	if balance := key.Balance; balance != nil {
		facts = append(facts, cli.Fact{Label: "balance", Text: usdText(balance.BalanceUSD) + " (" + usdText(balance.CreditsUSD) + " bought, " + usdText(balance.UsedUSD) + " used)"})
	}
	facts = append(facts, cli.Fact{Label: "balance", Text: key.BalanceUnread})
	for _, allowance := range key.Allowances {
		text := strconv.Itoa(allowance.Remaining) + " of " + strconv.Itoa(allowance.Limit) + " " + string(allowance.Unit) + " left, per " + string(allowance.Scope)
		if !allowance.ResetsAt.IsZero() {
			text += " · resets in " + widget.Until(allowance.ResetsAt.Sub(now))
		}
		facts = append(facts, cli.Fact{Label: "allowance", Text: text})
	}
	if failure := key.LastError; failure != nil {
		facts = append(facts, cli.Fact{Label: "last error", Text: strconv.Itoa(failure.Status) + " " + failure.What + " · " + widget.Until(now.Sub(failure.At)) + " ago"})
	}
	return append(facts, cli.Fact{Label: "reports", Text: page.Label(key.ProviderReports)})
}

func spendText(spend host.KeySpend) string {
	text := plural(spend.Calls, "call")
	if tokens := spend.Tokens; tokens.InputTokens+tokens.OutputTokens > 0 {
		text += " · " + strconv.Itoa(tokens.InputTokens) + " in, " + strconv.Itoa(tokens.OutputTokens) + " out"
		if tokens.CacheReadInputTokens+tokens.CacheWriteInputTokens > 0 {
			text += ", " + strconv.Itoa(tokens.CacheReadInputTokens) + " cache read, " + strconv.Itoa(tokens.CacheWriteInputTokens) + " cache write"
		}
		if tokens.ReasoningOutputTokens > 0 {
			text += ", " + strconv.Itoa(tokens.ReasoningOutputTokens) + " reasoning"
		}
		text += " tokens"
	}
	switch spend.CostSource {
	case host.CostProvider:
		text += " · " + usdText(spend.CostUSD) + " reported by the provider"
	case host.CostList:
		text += " · " + usdText(spend.CostUSD) + " at list price, estimated"
	case host.CostNone:
		text += " · no price"
	}
	if spend.UpstreamUSD > 0 {
		text += " · " + usdText(spend.UpstreamUSD) + " charged upstream"
	}
	return text
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

func accountCard(page cli.Page, source string, account accountStatus, now time.Time) ([]string, bool) {
	verdict, reason := stateLook(account.State)
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
