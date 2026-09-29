package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
)

type importRule struct {
	from  string
	scope string
	allow []string
}

func importRules() []importRule {
	return []importRule{
		{from: "tofu/cmd", scope: "tofu/bench"},
		{from: "tofu/interface", scope: "tofu/bench"},
		{from: "tofu/internal", scope: "tofu/bench"},
		{from: "tofu/internal", scope: "tofu/cmd"},
		{from: "tofu/internal", scope: "tofu/interface"},
		{from: "tofu/library", scope: "tofu"},
		{from: "tofu/internal/konst", scope: "tofu"},
		{from: "tofu/internal/sys", scope: "tofu"},
		{
			from:  "tofu/internal/judge",
			scope: "tofu/internal",
			allow: []string{"tofu/internal/judge", "tofu/internal/sys", "tofu/internal/konst", "tofu/internal/transport"},
		},
		{
			from:  "tofu/internal/point",
			scope: "tofu/internal",
			allow: []string{"tofu/internal/judge", "tofu/internal/rule", "tofu/internal/transform", "tofu/internal/sys", "tofu/internal/konst"},
		},
	}
}

func (r importRule) String() string {
	if len(r.allow) == 0 {
		return "nothing under " + r.from + " imports " + r.scope
	}
	return "under " + r.scope + ", a package in " + r.from + " imports only " + strings.Join(r.allow, ", ")
}

func (r importRule) broken(from, to string) bool {
	if !under(from, r.from) || !under(to, r.scope) {
		return false
	}
	for _, allowed := range r.allow {
		if under(to, allowed) {
			return false
		}
	}
	return true
}

func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func (r importRule) short() string {
	if len(r.allow) == 0 {
		return "never " + r.scope
	}
	names := make([]string, len(r.allow))
	for i, allowed := range r.allow {
		names[i] = strings.TrimPrefix(allowed, r.scope+"/")
	}
	return r.scope + " only " + strings.Join(names, ", ")
}

type importViolation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rule string `json:"rule"`
}

type packageImports struct {
	ImportPath string
	Imports    []string
}

func moduleImports(dir string) ([]packageImports, error) {
	list := exec.Command("go", "list", "-e", "-json=ImportPath,Imports", "./...")
	list.Dir = dir
	var stderr strings.Builder
	list.Stderr = &stderr
	stdout, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	var pkgs []packageImports
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	for decoder.More() {
		var pkg packageImports
		if err := decoder.Decode(&pkg); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

type importsReport struct {
	Packages   int               `json:"packages"`
	Rules      []string          `json:"rules"`
	Violations []importViolation `json:"violations"`
}

func readImports(dir string) (importsReport, error) {
	pkgs, err := moduleImports(dir)
	if err != nil {
		return importsReport{}, err
	}
	report := importsReport{Packages: len(pkgs), Violations: []importViolation{}}
	rules := importRules()
	for _, rule := range rules {
		report.Rules = append(report.Rules, rule.String())
	}
	for _, pkg := range pkgs {
		for _, imported := range pkg.Imports {
			for _, rule := range rules {
				if rule.broken(pkg.ImportPath, imported) {
					report.Violations = append(report.Violations, importViolation{From: pkg.ImportPath, To: imported, Rule: rule.String()})
				}
			}
		}
	}
	return report, nil
}

func doctorImports(dir string, asJSON bool, out, errOut io.Writer) int {
	now := time.Now()
	report, err := readImports(dir)
	if err != nil {
		return printFailure(errOut, exitUsage, "tofu doctor --imports: unreadable: "+err.Error(), "")
	}
	if asJSON {
		err = writeJSON(out, cli.Envelope{Verb: "doctor --imports", OK: len(report.Violations) == 0, At: now, Data: report, Problems: importProblems(report)})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, importsPage(page, report))
	}
	switch {
	case err != nil:
		return printFailure(errOut, exitVerdict, "tofu doctor --imports: "+err.Error(), "")
	case len(report.Violations) > 0:
		return exitVerdict
	}
	return exitOK
}

func importProblems(report importsReport) []cli.Problem {
	problems := make([]cli.Problem, len(report.Violations))
	for i, violation := range report.Violations {
		problems[i] = cli.Problem{What: violation.From + " imports " + violation.To + ", and " + violation.Rule}
	}
	return problems
}

func importsPage(page cli.Page, report importsReport) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "no violations"}
	if broken := len(report.Violations); broken > 0 {
		verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(broken) + " violations"}
	}
	facts := []string{strconv.Itoa(report.Packages) + " packages", strconv.Itoa(len(report.Rules)) + " rules"}
	lines := append(page.Title("Imports", facts, verdict), "", page.Section("rules", cli.Verdict{}))
	rows := make([]cli.Row, 0, len(report.Rules))
	for _, rule := range importRules() {
		row := cli.Row{Mark: cli.Done, Cells: []string{rule.from, rule.short()}}
		for _, violation := range report.Violations {
			if violation.Rule == rule.String() {
				row.Mark = cli.Fail
			}
		}
		rows = append(rows, row)
	}
	lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	if len(report.Violations) == 0 {
		return lines
	}
	rows = make([]cli.Row, len(report.Violations))
	for i, violation := range report.Violations {
		rows[i] = cli.Row{Mark: cli.Fail, Cells: []string{violation.From, "imports " + violation.To}}
	}
	lines = append(lines, "", page.Section("violations", cli.Verdict{}))
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}
