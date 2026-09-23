package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"tofu/internal/sys"
)

type ruleFiredReport struct {
	Fires      []ruleFireRecord `json:"fires"`
	Blocked    int              `json:"blocked"`
	Unreadable int              `json:"unreadable_lines"`
}

func rulesFiredVerb(args []string, out, errOut io.Writer) int {
	asJSON := false
	day := ""
	for _, arg := range args {
		switch {
		case arg == jsonFlag:
			asJSON = true
		case strings.HasPrefix(arg, "-"):
			return rulesFail(errOut, fmt.Errorf("unknown argument %q", arg))
		case day != "":
			return rulesFail(errOut, fmt.Errorf("tofu rules fired takes one date, got %q and %q", day, arg))
		default:
			day = arg
		}
	}
	if day != "" {
		if _, err := time.Parse(time.DateOnly, day); err != nil {
			return rulesFail(errOut, fmt.Errorf("%q is not a date in the form 2006-01-02", day))
		}
	}
	dir, err := sys.LogDir()
	if err != nil {
		return rulesFail(errOut, err)
	}
	fires, unreadable, err := jsonlRecords[ruleFireRecord](dir, func(name string) bool {
		return strings.HasSuffix(name, rulesFireSuffix) && strings.HasPrefix(name, day)
	})
	if err != nil {
		return rulesFail(errOut, err)
	}
	report := ruleFiredReport{Fires: fires, Unreadable: unreadable}
	for _, fire := range fires {
		if fire.Blocked {
			report.Blocked++
		}
	}
	if asJSON {
		if err := writeJSON(out, report); err != nil {
			return rulesFail(errOut, err)
		}
		return exitOK
	}
	_, _ = fmt.Fprintf(out, "%d fires recorded, %d blocked\n", len(report.Fires), report.Blocked)
	for _, fire := range report.Fires {
		_, _ = fmt.Fprintf(out, "%s  %s  %s  %s  blocked=%t\n",
			fire.At.UTC().Format(time.RFC3339), fire.RuleID, fire.Target, fire.Mode, fire.Blocked)
	}
	if report.Unreadable > 0 {
		_, _ = fmt.Fprintf(out, "%d unreadable lines skipped\n", report.Unreadable)
	}
	return exitOK
}
