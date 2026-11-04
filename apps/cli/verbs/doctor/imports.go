package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
)

type importRule struct {
	from  string
	scope string
	allow []string
	apart bool
}

func importRules() []importRule {
	leaves := []string{"tofu/internal/sys", "tofu/internal/konst", "tofu/internal/platform/recover"}
	rules := []importRule{
		{from: "tofu/apps", scope: "tofu/bench"},
		{from: "tofu/internal", scope: "tofu/bench"},
		{from: "tofu/internal", scope: "tofu/apps"},
		{from: "tofu/apps/cli/wiring", scope: "tofu/apps/cli/verbs"},
		{from: "tofu/apps/cli/verbs", scope: "tofu/apps/cli/verbs", apart: true},
		{from: "tofu/library", scope: "tofu"},
		{from: "tofu/internal/judge", scope: "tofu/internal", allow: slices.Concat([]string{"tofu/internal/judge"}, leaves, []string{"tofu/internal/models/transport", "tofu/internal/models/classifiers/core", "tofu/internal/models/classifiers/providers"})},
	}
	for _, leaf := range leaves {
		rules = append(rules, importRule{from: leaf, scope: "tofu", allow: leaves})
	}
	return rules
}

func (r importRule) String() string {
	switch {
	case r.apart:
		return "each package under " + r.from + " imports no other one under " + r.scope
	case len(r.allow) == 0:
		return "nothing under " + r.from + " imports " + r.scope
	}
	return "under " + r.scope + ", a package in " + r.from + " imports only " + strings.Join(r.allow, ", ")
}

func (r importRule) broken(from, to string) bool {
	if !under(from, r.from) || !under(to, r.scope) {
		return false
	}
	if r.apart {
		own, _, _ := strings.Cut(strings.TrimPrefix(from, r.from+"/"), "/")
		return !under(to, r.from+"/"+own)
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
	switch {
	case r.apart:
		return "never another under " + r.scope
	case len(r.allow) == 0:
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

func (v importViolation) what() string {
	if v.To == "" {
		return "matches no package: " + v.Rule
	}
	return "imports " + v.To
}

func deadRule(rule importRule, pkgs []packageImports) (importViolation, bool) {
	for _, prefix := range []string{rule.from, rule.scope} {
		if !slices.ContainsFunc(pkgs, func(pkg packageImports) bool { return under(pkg.ImportPath, prefix) }) {
			return importViolation{From: prefix, Rule: rule.String()}, true
		}
	}
	return importViolation{}, false
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
	tofuTree := slices.ContainsFunc(pkgs, func(pkg packageImports) bool { return under(pkg.ImportPath, "tofu") })
	for _, rule := range rules {
		report.Rules = append(report.Rules, rule.String())
		if dead, ok := deadRule(rule, pkgs); ok && tofuTree {
			report.Violations = append(report.Violations, dead)
		}
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

func Imports(dir string, asJSON bool, out, errOut io.Writer) int {
	now := time.Now()
	report, err := readImports(dir)
	if err != nil {
		return output.PrintFailure(errOut, output.ExitUsage, "tofu doctor --imports: unreadable: "+err.Error(), "")
	}
	if asJSON {
		err = output.WriteJSON(out, cli.Envelope{Verb: "doctor --imports", OK: len(report.Violations) == 0, At: now, Data: report, Problems: importProblems(report)})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, importsPage(page, report))
	}
	switch {
	case err != nil:
		return output.PrintFailure(errOut, output.ExitVerdict, "tofu doctor --imports: "+err.Error(), "")
	case len(report.Violations) > 0:
		return output.ExitVerdict
	}
	return output.ExitOK
}

func importProblems(report importsReport) []cli.Problem {
	problems := make([]cli.Problem, len(report.Violations))
	for i, violation := range report.Violations {
		what := violation.From + " " + violation.what()
		if violation.To != "" {
			what += ", and " + violation.Rule
		}
		problems[i] = cli.Problem{What: what}
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
		rows[i] = cli.Row{Mark: cli.Fail, Cells: []string{violation.From, violation.what()}}
	}
	lines = append(lines, "", page.Section("violations", cli.Verdict{}))
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}
