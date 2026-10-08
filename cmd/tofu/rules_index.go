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
	RuleID   string           `json:"rule_id"`
	Fires    bool             `json:"fires"`
	Why      string           `json:"why"`
	Concern  string           `json:"concern"`
	Kind     string           `json:"kind"`
	Mode     string           `json:"mode"`
	Override *overrideListing `json:"override,omitempty"`
}

type ruleIndexReport struct {
	Origin     string             `json:"origin"`
	Task       string             `json:"task"`
	TaskKind   string             `json:"task_kind"`
	Role       string             `json:"role,omitempty"`
	Paths      []string           `json:"paths"`
	Frameworks []string           `json:"frameworks,omitempty"`
	Firing     int                `json:"firing"`
	Rules      []ruleIndexListing `json:"rules"`
}

func rulesIndexVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules index", usageLine: `tofu rules index "<task>" [path...] [--task kind] [--role orchestrator|sub-agent] [--library dir] [--dir project] [--json]`, out: out, errOut: errOut}
	kept := make([]string, 0, len(args))
	verb, role, roleAsked := rule.VerbNone, rule.RoleAny, false
	for i := 0; i < len(args); i++ {
		flag := args[i]
		if flag != "--task" && flag != "--role" {
			kept = append(kept, flag)
			continue
		}
		if i++; i >= len(args) {
			return o.usage(fmt.Errorf("%s needs a value", flag))
		}
		if flag == "--task" {
			verb = rule.Verb(args[i])
			continue
		}
		role, roleAsked = rule.Role(args[i]), true
	}
	switch verb {
	case rule.VerbNone, rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite:
	default:
		return o.usage(fmt.Errorf("--task is %s, %s, %s or %s, found %q", rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite, verb))
	}
	if roleAsked && role != rule.RoleOrchestrator && role != rule.RoleSubAgent {
		return o.usage(fmt.Errorf("--role is %s or %s, found %q", rule.RoleOrchestrator, rule.RoleSubAgent, role))
	}
	opts, err := parseRulesFlags(kept)
	if err == nil && len(opts.rest) == 0 {
		err = errors.New("no task")
	}
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	stack, err := stackRules(opts.library, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	rules := stack.rules
	report := ruleIndexReport{Origin: stack.origin, Task: opts.rest[0], TaskKind: string(verb), Role: string(role), Paths: opts.rest[1:], Rules: make([]ruleIndexListing, 0, len(rules))}
	if report.Frameworks, err = rule.Frameworks(opts.dir, report.Paths); err != nil {
		return o.fail(err)
	}
	task := rule.Task{Text: report.Task, Paths: report.Paths, Verb: verb, Role: role, Frameworks: report.Frameworks}
	for i, m := range rule.Index(rules, task) {
		if reaches, why := rules[i].ReachesDomain(rule.DomainDev, task); !reaches {
			m.Fires, m.Why = false, why
		}
		if m.Fires {
			report.Firing++
		}
		report.Rules = append(report.Rules, ruleIndexListing{RuleID: m.RuleID, Fires: m.Fires, Why: m.Why, Concern: string(rules[i].Concern), Kind: string(rules[i].Kind), Mode: rules[i].Mode.String(), Override: stack.applied(rules[i])})
	}
	for _, off := range stack.switchedOff() {
		report.Rules = append(report.Rules, ruleIndexListing{RuleID: off.Base.ID, Why: "never fires while it is switched off", Concern: string(off.Base.Concern), Kind: string(off.Base.Kind), Mode: string(rule.ModeOff), Override: off.listing()})
	}
	return o.done(true, report, report.lines)
}

func (report ruleIndexReport) lines(page cli.Page) []string {
	var concerns []string
	rows := make([]cli.Row, len(report.Rules))
	running := len(report.Rules)
	for i, r := range report.Rules {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{r.RuleID}, Detail: r.Why}
		if r.Override != nil {
			rows[i].Detail = overrideWhy(*r.Override) + factSeparator + r.Why
		}
		if r.Mode == string(rule.ModeOff) {
			rows[i].Mark = cli.Removed
			running--
		}
		if r.Fires {
			rows[i].Mark = cli.Active
			if !slices.Contains(concerns, r.Concern) {
				concerns = append(concerns, r.Concern)
			}
		}
	}
	verdict := cli.Verdict{Mark: cli.Idle, Text: "none of " + strconv.Itoa(running) + " fire"}
	if report.Firing > 0 {
		verdict = cli.Verdict{Mark: cli.Active, Text: strconv.Itoa(report.Firing) + " of " + strconv.Itoa(running) + " fire"}
	}
	lines := append(page.Title("Rules index", []string{"from " + page.Path(report.Origin)}, verdict), "")
	facts := []cli.Fact{
		{Label: "task", Text: report.Task},
		{Label: "kind", Text: report.TaskKind},
		{Label: "paths", Text: strings.Join(report.Paths, ", ")},
		{Label: "concerns", Text: strings.Join(concerns, ", ")},
	}
	if report.Role != "" {
		facts = append(facts, cli.Fact{Label: "role", Text: report.Role})
	}
	if len(report.Frameworks) > 0 {
		facts = append(facts, cli.Fact{Label: "frameworks", Text: strings.Join(report.Frameworks, ", ")})
	}
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(append(lines, ""), page.Rows(rows)...)
}
