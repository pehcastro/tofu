package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"tofu/internal/cli/output"
	"tofu/internal/cli/page"
	"tofu/internal/cli/render"
	"tofu/internal/cli/wiring"
	"tofu/internal/sys"
)

type ruleFiredReport struct {
	Fires      []ruleFireRecord `json:"fires"`
	Blocked    int              `json:"blocked"`
	Unreadable int              `json:"unreadable_lines"`
}

func rulesFiredVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules fired", UsageLine: "tofu rules fired [2006-01-02] [--json]", Out: out, ErrOut: errOut}
	day := ""
	for _, arg := range args {
		switch {
		case arg == output.JSONFlag:
			o.AsJSON = true
		case strings.HasPrefix(arg, "-"):
			return o.Usage(fmt.Errorf("unknown argument %q", arg))
		case day != "":
			return o.Usage(fmt.Errorf("one date, got %q and %q", day, arg))
		default:
			day = arg
		}
	}
	if day != "" {
		if _, err := time.Parse(time.DateOnly, day); err != nil {
			return o.Usage(fmt.Errorf("%q is not a date in the form 2006-01-02", day))
		}
	}
	dir, err := sys.LogDir()
	if err != nil {
		return o.Fail(err)
	}
	fires, unreadable, err := output.JSONLRecords[ruleFireRecord](dir, func(name string) bool {
		return strings.HasSuffix(name, rulesFireSuffix) && strings.HasPrefix(name, day)
	})
	if err != nil {
		return o.Fail(err)
	}
	report := ruleFiredReport{Fires: fires, Unreadable: unreadable}
	for _, fire := range fires {
		if fire.Blocked {
			report.Blocked++
		}
	}
	now := time.Now()
	return o.Done(true, report, func(page cli.Page) []string { return report.lines(page, day, now) })
}

func (report ruleFiredReport) lines(page cli.Page, day string, now time.Time) []string {
	var facts []string
	if day != "" {
		facts = []string{day}
	}
	lines := page.Title("Rules fired", facts, wiring.FiresVerdict(len(report.Fires), report.Blocked))
	rows := make([]cli.Row, len(report.Fires))
	for i, fire := range report.Fires {
		rows[i] = fire.row(page, widget.Until(now.Sub(fire.At))+" ago")
	}
	if len(rows) > 0 {
		lines = append(append(lines, ""), page.Rows(rows)...)
	}
	if report.Unreadable > 0 {
		lines = append(lines, "", page.Status("unreadable", cli.Verdict{Mark: cli.Warn, Text: output.Plural(report.Unreadable, "line") + " skipped"}))
	}
	return lines
}
