package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/rule"
)

type ruleIndexListing struct {
	RuleID  string `json:"rule_id"`
	Fires   bool   `json:"fires"`
	Why     string `json:"why"`
	Concern string `json:"concern"`
	Kind    string `json:"kind"`
	Mode    string `json:"mode"`
}

type ruleIndexReport struct {
	Origin     string             `json:"origin"`
	Task       string             `json:"task"`
	TaskKind   string             `json:"task_kind"`
	Paths      []string           `json:"paths"`
	Frameworks []string           `json:"frameworks,omitempty"`
	Firing     int                `json:"firing"`
	Rules      []ruleIndexListing `json:"rules"`
}

func rulesIndexVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules index", usageLine: `tofu rules index "<task>" [path...] [--task kind] [--library dir] [--dir project] [--json]`, out: out, errOut: errOut}
	kept := make([]string, 0, len(args))
	verb := rule.VerbNone
	for i := 0; i < len(args); i++ {
		if args[i] != "--task" {
			kept = append(kept, args[i])
			continue
		}
		if i++; i >= len(args) {
			return o.usage(errors.New("--task needs a value"))
		}
		verb = rule.Verb(args[i])
	}
	switch verb {
	case rule.VerbNone, rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite:
	default:
		return o.usage(fmt.Errorf("--task is %s, %s, %s or %s, found %q", rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite, verb))
	}
	opts, err := parseRulesFlags(kept)
	if err == nil && len(opts.rest) == 0 {
		err = errors.New("no task")
	}
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	rules, origin, err := loadRules(opts.library, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	report := ruleIndexReport{Origin: origin, Task: opts.rest[0], TaskKind: string(verb), Paths: opts.rest[1:], Rules: make([]ruleIndexListing, 0, len(rules))}
	if report.Frameworks, err = rule.Frameworks(opts.dir, report.Paths); err != nil {
		return o.fail(err)
	}
	for i, m := range rule.Index(rules, rule.Task{Text: report.Task, Paths: report.Paths, Verb: verb, Frameworks: report.Frameworks}) {
		if m.Fires {
			report.Firing++
		}
		report.Rules = append(report.Rules, ruleIndexListing{RuleID: m.RuleID, Fires: m.Fires, Why: m.Why, Concern: string(rules[i].Concern), Kind: string(rules[i].Kind), Mode: rules[i].Mode.String()})
	}
	return o.done(true, report, report.lines)
}

func (report ruleIndexReport) lines(page cli.Page) []string {
	verdict := cli.Verdict{Mark: cli.Idle, Text: "none of " + strconv.Itoa(len(report.Rules)) + " fire"}
	if report.Firing > 0 {
		verdict = cli.Verdict{Mark: cli.Active, Text: strconv.Itoa(report.Firing) + " of " + strconv.Itoa(len(report.Rules)) + " fire"}
	}
	var concerns []string
	rows := make([]cli.Row, len(report.Rules))
	for i, r := range report.Rules {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{r.RuleID}, Detail: r.Why}
		if r.Fires {
			rows[i].Mark = cli.Active
			if !slices.Contains(concerns, r.Concern) {
				concerns = append(concerns, r.Concern)
			}
		}
	}
	lines := append(page.Title("Rules index", []string{"from " + page.Path(report.Origin)}, verdict), "")
	facts := []cli.Fact{
		{Label: "task", Text: report.Task},
		{Label: "kind", Text: report.TaskKind},
		{Label: "paths", Text: strings.Join(report.Paths, ", ")},
		{Label: "concerns", Text: strings.Join(concerns, ", ")},
	}
	if len(report.Frameworks) > 0 {
		facts = append(facts, cli.Fact{Label: "frameworks", Text: strings.Join(report.Frameworks, ", ")})
	}
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(append(lines, ""), page.Rows(rows)...)
}
