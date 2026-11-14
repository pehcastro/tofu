package rules

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/host"
	rule "tofu/internal/librarian/rules"
	"tofu/internal/project/boardy"
)

type IndexListing struct {
	RuleID   string                `json:"rule_id"`
	Fires    bool                  `json:"fires"`
	Why      string                `json:"why"`
	Concern  string                `json:"concern"`
	Kind     string                `json:"kind"`
	Mode     string                `json:"mode"`
	Override *host.OverrideListing `json:"override,omitempty"`
}

type IndexReport struct {
	Origin     string         `json:"origin"`
	Task       string         `json:"task"`
	TaskKind   string         `json:"task_kind"`
	Role       string         `json:"role,omitempty"`
	Agent      string         `json:"agent,omitempty"`
	Paths      []string       `json:"paths"`
	Frameworks []string       `json:"frameworks,omitempty"`
	DecidedBy  string         `json:"decided_by,omitempty"`
	Firing     int            `json:"firing"`
	Rules      []IndexListing `json:"rules"`
}

func indexVerb(args []string, out, errOut io.Writer, agentsIn func(dir string) (subagents.Found, error)) int {
	o := output.Verb{Name: "rules index", UsageLine: `tofu rules index "<task>" [path...] [--task kind] [--role orchestrator|sub-agent] [--agent name] [--library dir] [--dir project] [--json]`, Out: out, ErrOut: errOut}
	kept := make([]string, 0, len(args))
	verb, role, agentName, verbAsked, roleAsked := rule.VerbNone, rule.RoleAny, "", false, false
	for i := 0; i < len(args); i++ {
		flag := args[i]
		if flag != "--task" && flag != "--role" && flag != "--agent" {
			kept = append(kept, flag)
			continue
		}
		if i++; i >= len(args) {
			return o.Usage(fmt.Errorf("%s needs a value", flag))
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
		return o.Usage(fmt.Errorf("--task is %s, %s, %s, %s or %s, found %q", rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite, rule.VerbPlan, verb))
	}
	if roleAsked && role != rule.RoleOrchestrator && role != rule.RoleSubAgent {
		return o.Usage(fmt.Errorf("--role is %s or %s, found %q", rule.RoleOrchestrator, rule.RoleSubAgent, role))
	}
	if agentName != "" && role == rule.RoleOrchestrator {
		return o.Usage(fmt.Errorf("--agent %s composes a sub-agent's prompt, and --role is %s", agentName, role))
	}
	opts, err := parseFlags(kept)
	if err == nil && len(opts.rest) == 0 {
		err = errors.New("no task")
	}
	if err != nil {
		return o.Usage(err)
	}
	o.AsJSON = opts.json
	boards, opened := boardy.OpenStore(opts.dir)
	stack, err := wiring.StackRules(opts.library, boards, opened)
	if err != nil {
		return o.Fail(err)
	}
	var agent subagents.Definition
	if agentName != "" {
		found, err := agentsIn(opts.dir)
		if err == nil {
			agent, err = loop.SubAgents{Defined: found.Definitions}.Named(agentName)
		}
		if err != nil {
			return o.Fail(err)
		}
		role = rule.RoleSubAgent
	}
	rules := stack.Rules
	task := loop.TaskNamed(opts.rest[0])
	if verbAsked {
		task.Verb = verb
	}
	for _, named := range opts.rest[1:] {
		if !slices.Contains(task.Paths, named) {
			task.Paths = append(task.Paths, named)
		}
	}
	report := IndexReport{Origin: stack.Origin, Task: task.Text, TaskKind: string(task.Verb), Role: string(role), Agent: agent.Name, Paths: task.Paths, Rules: make([]IndexListing, 0, len(rules))}
	if report.Frameworks, err = rule.Frameworks(opts.dir, report.Paths); err != nil {
		return o.Fail(err)
	}
	task.Role, task.Language, task.Frameworks = role, agent.Language, report.Frameworks
	domain := cmp.Or(agent.Domain, rule.DomainDev)
	if asked := rule.AskedOfTheTask(rules, domain, task); len(asked) > 0 {
		report.DecidedBy = "Jev, on " + strconv.Itoa(len(asked)) + " rules that declare touches"
		if task.Judged, err = loop.NewRuleJudge(opts.dir).Of(task, asked); err != nil {
			report.DecidedBy = "the touches keyword, since " + err.Error()
		}
	}
	for i, m := range rule.IndexReaching(rules, domain, task) {
		if m.Fires {
			report.Firing++
		}
		report.Rules = append(report.Rules, IndexListing{RuleID: m.RuleID, Fires: m.Fires, Why: m.Why, Concern: string(rules[i].Concern), Kind: string(rules[i].Kind), Mode: rules[i].Mode.String(), Override: stack.Applied(rules[i])})
	}
	for _, off := range stack.SwitchedOff() {
		report.Rules = append(report.Rules, IndexListing{RuleID: off.Base.ID, Why: "never fires while it is switched off", Concern: string(off.Base.Concern), Kind: string(off.Base.Kind), Mode: string(rule.ModeOff), Override: off.Listing()})
	}
	return o.Done(true, report, report.lines)
}

func (report IndexReport) lines(page cli.Page) []string {
	var concerns []string
	rows := make([]cli.Row, len(report.Rules))
	running := len(report.Rules)
	for i, r := range report.Rules {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{r.RuleID}, Detail: r.Why}
		if r.Override != nil {
			rows[i].Detail = wiring.OverrideWhy(*r.Override) + output.FactSeparator + r.Why
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
