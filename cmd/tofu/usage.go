package main

import (
	"cmp"
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/llm/quota"
	"tofu/internal/widget"
)

const (
	usageFlags            = "tofu usage [--history] [--json]"
	historyFlag           = "--history"
	usageNoWindowReported = "no window is reporting use"
)

type usageReport = host.UsageReport

const (
	usageServing   = host.UsageServing
	usageAttention = host.UsageAttention
	usageNone      = host.UsageNone
)

func usageVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "usage", usageLine: usageFlags, asJSON: jsonAsked(args), out: out, errOut: errOut}
	for _, arg := range args {
		if arg != jsonFlag && arg != historyFlag {
			return o.usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	if slices.Contains(args, historyFlag) {
		return usageHistoryVerb(out, errOut, o.asJSON)
	}
	now := time.Now()
	report, err := readUsage(now)
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, report, func(page cli.Page) []string { return usagePage(page, report, now) })
}

func usageFail(errOut io.Writer, err error) int {
	return printFailure(errOut, exitVerdict, "tofu usage: "+err.Error(), "")
}

func readUsage(now time.Time) (usageReport, error) {
	results, err := pollCredentials(context.Background(), time.Now, nil)
	if err != nil {
		return usageReport{}, err
	}
	report := usageReport{State: usageServing, Providers: credentialReports(results, now), SpendLimit: quota.SpendLimitLine()}
	if len(results) == 0 {
		report.State, report.Missing = usageNone, doctorBlockers()
		return report, nil
	}
	fullest := -1.0
	for _, provider := range report.Providers {
		if provider.State != usageServingState {
			report.State = usageAttention
		}
		for _, window := range provider.Windows {
			if window.Reported && window.Used > fullest {
				fullest, report.Fullest = window.Used, provider.Provider+" "+window.ID
			}
		}
	}
	return report, nil
}

func usagePage(page cli.Page, report usageReport, now time.Time) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: string(report.State)}
	switch report.State {
	case usageServing:
	case usageAttention:
		verdict.Mark = cli.Warn
	case usageNone:
		lines := append(page.Title("Usage", nil, cli.Verdict{Mark: cli.Idle, Text: "none signed in"}), "")
		return append(lines, blockerRows(page, report.Missing)...)
	}
	lines := page.Title("Usage", []string{strconv.Itoa(len(report.Providers)) + " signed in"}, verdict)
	for i, provider := range report.Providers {
		lines = append(lines, "")
		if i == 0 || provider.Provider != report.Providers[i-1].Provider {
			lines = append(lines, page.Section(provider.Provider, cli.Verdict{}))
		}
		cardVerdict := cli.Verdict{Mark: cli.Active, Text: usageServingState}
		var facts []cli.Fact
		if provider.State != usageServingState {
			cardVerdict = cli.Verdict{Mark: cli.Warn, Text: string(usageAttention)}
			facts = append(facts, cli.Fact{Label: "state", Text: provider.State})
		}
		for _, window := range provider.Windows {
			text := page.Label(windowPercent(window))
			if window.Reported {
				text = page.Bar(window.Used)
				if left := window.ResetsAt.Sub(now); left > 0 {
					text += cli.Gap + page.Label("resets in "+widget.Until(left))
				}
			}
			facts = append(facts, cli.Fact{Label: window.ID, Text: text})
		}
		lines = append(lines, page.Card(page.Subject(cmp.Or(provider.Plan, "plan not reported")), cardVerdict, page.Facts(facts))...)
	}
	return lines
}
