package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/cli/page"
	"tofu/internal/librarian/rules"
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
	Agent      string             `json:"agent,omitempty"`
	Paths      []string           `json:"paths"`
	Frameworks []string           `json:"frameworks,omitempty"`
	DecidedBy  string             `json:"decided_by,omitempty"`
	Firing     int                `json:"firing"`
	Rules      []ruleIndexListing `json:"rules"`
}

func rulesIndexVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules index", usageLine: `tofu rules index "<task>" [path...] [--task kind] [--role orchestrator|sub-agent] [--agent name] [--library dir] [--dir project] [--json]`, out: out, errOut: errOut}
	kept := make([]string, 0, len(args))
	verb, role, agentName, verbAsked, roleAsked := rule.VerbNone, rule.RoleAny, "", false, false
	for i := 0; i < len(args); i++ {
		flag := args[i]
		if flag != "--task" && flag != "--role" && flag != "--agent" {
			kept = append(kept, flag)
			continue
		}
		if i++; i >= len(args) {
			return o.usage(fmt.Errorf("%s needs a value", flag))
		}
		switch flag {
		case "--task":
			verb, verbAsked = rule.Verb(args[i]), true
		case "--role":
			role, roleAsked = rule.Role(args[i]), true
		case "--agent":
			agentName = args[i]
		}
	}
	switch verb {
	case rule.VerbNone, rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite, rule.VerbPlan:
	default:
		return o.usage(fmt.Errorf("--task is %s, %s, %s, %s or %s, found %q", rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite, rule.VerbPlan, verb))
	}
	if roleAsked && role != rule.RoleOrchestrator && role != rule.RoleSubAgent {
		return o.usage(fmt.Errorf("--role is %s or %s, found %q", rule.RoleOrchestrator, rule.RoleSubAgent, role))
	}
	if agentName != "" && role == rule.RoleOrchestrator {
		return o.usage(fmt.Errorf("--agent %s composes a sub-agent's prompt, and --role is %s", agentName, role))
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
	var agent subagent.Definition
	if agentName != "" {
		found, err := agentsIn(opts.dir)
		if err == nil {
			agent, err = turn.SubAgents{Defined: found.Definitions}.Named(agentName)
		}
		if err != nil {
			return o.fail(err)
		}
		role = rule.RoleSubAgent
	}
	rules := stack.rules
	task := turn.TaskNamed(opts.rest[0])
	if verbAsked {
		task.Verb = verb
	}
	for _, named := range opts.rest[1:] {
		if !slices.Contains(task.Paths, named) {
			task.Paths = append(task.Paths, named)
		}
	}
	report := ruleIndexReport{Origin: stack.origin, Task: task.Text, TaskKind: string(task.Verb), Role: string(role), Agent: agent.Name, Paths: task.Paths, Rules: make([]ruleIndexListing, 0, len(rules))}
	if report.Frameworks, err = rule.Frameworks(opts.dir, report.Paths); err != nil {
		return o.fail(err)
	}
	task.Role, task.Language, task.Frameworks = role, agent.Language, report.Frameworks
	domain := cmp.Or(agent.Domain, rule.DomainDev)
	if asked := rule.AskedOfTheTask(rules, domain, task); len(asked) > 0 {
		report.DecidedBy = "Jev, on " + strconv.Itoa(len(asked)) + " rules that declare touches"
		if task.Judged, err = turn.NewRuleJudge(opts.dir).Of(task, asked); err != nil {
			report.DecidedBy = "the touches keyword, since " + err.Error()
		}
	}
	for i, m := range rule.IndexReaching(rules, domain, task) {
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
	if report.Agent != "" {
		facts = append(facts, cli.Fact{Label: "agent", Text: report.Agent})
	}
	if len(report.Frameworks) > 0 {
		facts = append(facts, cli.Fact{Label: "frameworks", Text: strings.Join(report.Frameworks, ", ")})
	}
	if report.DecidedBy != "" {
		facts = append(facts, cli.Fact{Label: "decided by", Text: report.DecidedBy})
	}
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(append(lines, ""), page.Rows(rows)...)
}
