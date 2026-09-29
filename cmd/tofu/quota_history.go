package main

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
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
	now := time.Now()
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
	report := quotaHistoryReport{Readings: []quotaHistoryRow{}, Unreadable: unreadable}
	for _, reading := range recorded {
		for _, window := range reading.Windows {
			report.Readings = append(report.Readings, quotaHistoryRow{At: reading.At.UTC(), Provider: string(reading.Provider), Window: window.ID, Used: window.Used})
		}
	}
	var problems []cli.Problem
	if unreadable > 0 {
		problems = []cli.Problem{{What: strconv.Itoa(unreadable) + " unreadable lines skipped"}}
	}
	if asJSON {
		err = writeJSON(out, cli.Envelope{Verb: "usage --history", OK: len(problems) == 0, At: now, Data: report, Problems: problems})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, historyPage(page, report, now))
	}
	if err != nil {
		return usageFail(errOut, err)
	}
	return exitOK
}

func historyPage(page cli.Page, report quotaHistoryReport, now time.Time) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "all read"}
	switch {
	case report.Unreadable > 0:
		verdict = cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(report.Unreadable) + " unreadable lines"}
	case len(report.Readings) == 0:
		return page.Title("Usage history", nil, cli.Verdict{Mark: cli.Idle, Text: "none recorded"})
	}
	rows := make([]cli.Row, len(report.Readings))
	for i, reading := range report.Readings {
		rows[i] = cli.Row{Cells: []string{reading.Provider, reading.Window, page.Bar(reading.Used)}, Detail: widget.Until(now.Sub(reading.At)) + " ago"}
	}
	lines := append(page.Title("Usage history", []string{strconv.Itoa(len(report.Readings)) + " readings"}, verdict), "")
	return append(lines, page.Rows(rows)...)
}
