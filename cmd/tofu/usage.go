package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/internal/llm/quota"
	"tofu/internal/widget"
)

const (
	usageFlags            = "tofu usage [--history] [--json]"
	historyFlag           = "--history"
	usageNoWindowReported = "no window is reporting use"
)

type usageState int

const (
	usageServing usageState = iota
	usageAttention
	usageNone
)

func (s usageState) String() string {
	switch s {
	case usageServing:
		return usageServingState
	case usageAttention:
		return "needs attention"
	case usageNone:
		return "none"
	}
	panic("tofu usage: unknown state")
}

//nolint:unparam
func (s usageState) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(s.String())), nil }

func (s *usageState) UnmarshalJSON(data []byte) error {
	text, err := strconv.Unquote(string(data))
	if err != nil {
		return err
	}
	for _, candidate := range []usageState{usageServing, usageAttention, usageNone} {
		if candidate.String() == text {
			*s = candidate
			return nil
		}
	}
	return fmt.Errorf("tofu usage: unknown state %q", text)
}

type usageReport struct {
	State      usageState         `json:"state"`
	Fullest    string             `json:"fullest_window,omitempty"`
	Providers  []credentialReport `json:"providers"`
	SpendLimit string             `json:"spend_limit"`
}

func usageVerb(args []string, out, errOut io.Writer) int {
	asJSON, history := false, false
	for _, arg := range args {
		switch arg {
		case jsonFlag:
			asJSON = true
		case historyFlag:
			history = true
		default:
			return printFailure(errOut, exitUsage, "tofu usage: unknown argument "+strconv.Quote(arg), usageFlags)
		}
	}
	if history {
		return usageHistoryVerb(out, errOut, asJSON)
	}
	now := time.Now()
	report, err := readUsage(now)
	if err != nil {
		return usageFail(errOut, err)
	}
	var problems []cli.Problem
	for _, provider := range report.Providers {
		if provider.State != usageServingState {
			problems = append(problems, cli.Problem{What: provider.Provider + ": " + provider.State})
		}
	}
	if report.State == usageNone {
		problems = blockerProblems(doctorBlockers())
	}
	if asJSON {
		err = writeJSON(out, cli.Envelope{Verb: "usage", OK: len(problems) == 0, At: now, Data: report, Problems: problems})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, usagePage(page, report, now))
	}
	if err != nil {
		return usageFail(errOut, err)
	}
	return exitOK
}

func usageFail(errOut io.Writer, err error) int {
	return printFailure(errOut, exitVerdict, "tofu usage: "+err.Error(), "")
}

func readUsage(now time.Time) (usageReport, error) {
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return usageReport{}, err
	}
	report := usageReport{State: usageServing, Providers: credentialReports(results, now), SpendLimit: quota.SpendLimitLine()}
	if len(results) == 0 {
		report.State = usageNone
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
	verdict := cli.Verdict{Mark: cli.Done, Text: report.State.String()}
	switch report.State {
	case usageServing:
	case usageAttention:
		verdict.Mark = cli.Warn
	case usageNone:
		lines := append(page.Title("Usage", nil, cli.Verdict{Mark: cli.Idle, Text: "none signed in"}), "")
		return append(lines, blockerRows(page, doctorBlockers())...)
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
			cardVerdict = cli.Verdict{Mark: cli.Warn, Text: usageAttention.String()}
			facts = append(facts, cli.Fact{Label: "state", Text: provider.State})
		}
		for _, window := range provider.Windows {
			text := page.Label(window.percent())
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
