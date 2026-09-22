package report

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	DataGlobal = "window.BENCH"
	DataScript = "reports.js"
	DataFile   = "reports.json"
	ViewerFile = "index.html"
)

type Data struct {
	Reports           []Report  `json:"reports"`
	NoReport          []Package `json:"benches_with_no_dated_report"`
	Counts            Counts    `json:"counts"`
	Dates             Chart     `json:"dates"`
	WithdrawalsLiveIn string    `json:"withdrawals_declared_from_outside_live_in"`
	GeneratedBy       string    `json:"generated_by"`
}

func (d Data) JSON() ([]byte, error) {
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func (d Data) JS() ([]byte, error) {
	body, err := d.JSON()
	if err != nil {
		return nil, err
	}
	return []byte(DataGlobal + " = " + strings.TrimRight(string(body), "\n") + ";\n"), nil
}

func (d Data) Markdown() string {
	doc := &strings.Builder{}
	doc.WriteString("# What bench has measured\n\n")
	fmt.Fprintf(doc, "%d dated reports under `bench/`, over %d benches. %d benches carry none. %d are withdrawn whole or in part, %d is stale, and %d name no conclusion a reader can find.\n\n",
		d.Counts.Reports, d.Counts.MeasuredPackage, d.Counts.NoReport, d.Counts.Withdrawn, d.Counts.Stale, d.Counts.Unparsed)
	fmt.Fprintf(doc, "This file is generated. Run `%s` to rewrite it, or open `bench/report/%s`, which is the same data with a viewer over it. A withdrawal declared from outside a report lives in `%s`; every other state below is declared by the report's own first lines.\n\n",
		d.GeneratedBy, ViewerFile, d.WithdrawalsLiveIn)
	doc.WriteString("## Every dated report, newest first\n\n")
	doc.WriteString("| Bench | Date | Report | Headline | What it found | Sample | State |\n|---|---|---|---|---|---|---|\n")
	for _, report := range d.Reports {
		fmt.Fprintf(doc, "| %s | %s | `%s` | %s | %s | %s | %s |\n",
			report.Package, report.Date, report.Source, cell(report.Figure), cell(report.Conclusion), cell(report.Sample), report.State)
	}
	doc.WriteString("\n## Not standing\n\n")
	for _, report := range d.Reports {
		if report.State.Stands() {
			continue
		}
		fmt.Fprintf(doc, "- `%s` is %s, said by %s: %s\n", report.Source, report.State, report.StateSource, cell(report.StateNote))
	}
	doc.WriteString("\n## Benches with no dated report\n\n")
	for _, pkg := range d.NoReport {
		fmt.Fprintf(doc, "- `bench/%s`, %s: %s\n", pkg.Package, pkg.Kind, pkg.Note)
	}
	doc.WriteString("\n## Where these reports came from\n\n")
	prose, rerun := 0, 0
	for _, report := range d.Reports {
		if report.BuiltFrom == FromRerun {
			rerun++
			continue
		}
		prose++
	}
	fmt.Fprintf(doc, "%d of %d are built by running the package's own code again. %d are built from the markdown's own text, which is weaker evidence, and every one of them says so.\n",
		rerun, len(d.Reports), prose)
	return doc.String()
}

func cell(text string) string {
	if text == "" {
		return "not stated"
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "|", "\\|"), "\n", " ")
}
