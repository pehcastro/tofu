package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"tofu/internal/llm/quota"
)

const (
	usageFlags        = "usage: tofu usage [--json]"
	usageNoCredential = "no subscription credential is stored"
	spendLimitPrefix  = "spend limit: "
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
	Blockers   []doctorBlocker    `json:"blockers,omitempty"`
	SpendLimit string             `json:"spend_limit"`
	ReportedAt time.Time          `json:"reported_at"`
}

func usageVerb(args []string, out, errOut io.Writer, shade palette) int {
	asJSON := false
	for _, arg := range args {
		if arg != jsonFlag {
			_, _ = fmt.Fprintln(errOut, usageFlags)
			return exitUsage
		}
		asJSON = true
	}
	now := time.Now()
	report, err := readUsage(now)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu usage: %v\n", err)
		return exitVerdict
	}
	if !asJSON {
		_, _ = fmt.Fprint(out, usageText(report, shade, now))
		return exitOK
	}
	if err := writeJSON(out, report); err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu usage: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func readUsage(now time.Time) (usageReport, error) {
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return usageReport{}, err
	}
	report := usageReport{
		State:      usageServing,
		Providers:  credentialReports(results, now),
		SpendLimit: quota.SpendLimitLine(),
		ReportedAt: now,
	}
	if len(results) == 0 {
		report.State, report.Blockers = usageNone, doctorBlockers()
		return report, nil
	}
	fullest := -1.0
	for _, provider := range report.Providers {
		if provider.State != usageServingState {
			report.State = usageAttention
		}
		for _, window := range provider.Windows {
			if !window.Reported || window.Used <= fullest {
				continue
			}
			fullest = window.Used
			report.Fullest = provider.Provider + " " + window.ID
		}
	}
	return report, nil
}

func usageText(report usageReport, shade palette, now time.Time) string {
	state := report.State.String()
	painted := shade.settled(state)
	if report.State != usageServing {
		painted = shade.unsettled(state)
	}
	head := usageNoCredential
	var body strings.Builder
	for _, provider := range report.Providers {
		label := provider.Provider
		if provider.State != usageServingState {
			body.WriteString(strings.Join(wrapped(label, provider.State), "\n") + "\n")
			continue
		}
		for _, window := range provider.Windows {
			if !window.Reported {
				continue
			}
			shown := plain
			if provider.Provider+" "+window.ID == report.Fullest {
				head = report.Fullest + " is the fullest at " + window.percent()
				shown = shade
			}
			body.WriteString(labelled(label, window.text(shown, now)) + "\n")
			label = ""
		}
	}
	for _, blocker := range report.Blockers {
		body.WriteString(strings.Join(blockerLines(blocker.Label, blocker.What, blocker.Command), "\n") + "\n")
	}
	limit := wrapped("spend", strings.TrimPrefix(report.SpendLimit, spendLimitPrefix))
	return headline(head, painted, len(state)) + "\n\n" + body.String() + "\n" + strings.Join(limit, "\n") + "\n"
}
