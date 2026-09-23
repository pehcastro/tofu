package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"tofu/internal/llm/quota"
	"tofu/internal/widget"
)

type quotaHistoryRow struct {
	At       time.Time `json:"at"`
	Provider string    `json:"provider"`
	Window   string    `json:"window"`
	Used     float64   `json:"used_fraction"`
}

type quotaHistoryReport struct {
	Readings   []quotaHistoryRow `json:"readings"`
	Unreadable int               `json:"unreadable_lines"`
}

func usageHistoryVerb(out, errOut io.Writer, asJSON bool) int {
	dir, err := quotaReadingDir()
	if err != nil {
		return usageFail(errOut, err)
	}
	recorded, unreadable, err := jsonlRecords[quota.Reading](dir, func(name string) bool {
		return strings.HasSuffix(name, jsonLinesSuffix)
	})
	if err != nil {
		return usageFail(errOut, err)
	}
	report := quotaHistoryReport{Unreadable: unreadable}
	for _, reading := range recorded {
		for _, window := range reading.Windows {
			report.Readings = append(report.Readings, quotaHistoryRow{
				At:       reading.At.UTC(),
				Provider: string(reading.Provider),
				Window:   window.ID,
				Used:     window.Used,
			})
		}
	}
	if asJSON {
		if err := writeJSON(out, report); err != nil {
			return usageFail(errOut, err)
		}
		return exitOK
	}
	_, _ = fmt.Fprintf(out, "%d readings recorded\n", len(report.Readings))
	for _, reading := range report.Readings {
		_, _ = fmt.Fprintf(out, "%s  %s  %s  %s\n",
			reading.At.Format(time.RFC3339), reading.Provider, reading.Window, widget.Percent(reading.Used))
	}
	if report.Unreadable > 0 {
		_, _ = fmt.Fprintf(out, "%d unreadable lines skipped\n", report.Unreadable)
	}
	return exitOK
}
