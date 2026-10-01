package main

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

const (
	agentsAddUsage    = "tofu agents add [--global] [--dir project] <name> --description d --model source/model [--tools a,b]"
	agentsSetUsage    = "tofu agents set [--global] [--dir project] <name> <source/model>"
	agentsRemoveUsage = "tofu agents remove [--global] [--dir project] <name>"
	agentsUsage       = "tofu agents [--json] | add | set | remove"
)

func agentsVerb(args []string, out, errOut io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			return agentsAddVerb(args[1:], out, errOut)
		case "set":
			return agentsSetVerb(args[1:], out, errOut)
		case "remove":
			return agentsRemoveVerb(args[1:], out, errOut)
		}
	}
	o := verbOutput{verb: "agents", usageLine: agentsUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	if unknown := withoutJSON(args); len(unknown) > 0 {
		return o.usage(errors.New("unknown argument " + strconv.Quote(unknown[0])))
	}
	found, err := discoverAgents()
	if err != nil {
		return o.fail(err)
	}
	problems := make([]cli.Problem, len(found.Broken))
	for i, broken := range found.Broken {
		problems[i] = cli.Problem{What: broken.Path + ": " + broken.Reason}
	}
	return show(out, o.asJSON, cli.Envelope{Verb: o.verb, OK: len(problems) == 0, At: time.Now(), Data: found, Problems: problems},
		func(page cli.Page) []string { return agentsPage(page, found) })
}

func discoverAgents() (subagent.Found, error) {
	dir, err := os.Getwd()
	if err != nil {
		return subagent.Found{}, err
	}
	return agentsIn(dir)
}

func agentsIn(dir string) (subagent.Found, error) {
	named, _, err := assembleRunTools(dir, toolSetFull, nil, &turn.BashTool{})
	if err != nil {
		return subagent.Found{}, err
	}
	return scanSubAgents(dir, named), nil
}

func agentSource(origin string) string {
	switch origin {
	case "library":
		return "library"
	case "~/.tofu":
		return "global"
	}
	return "project"
}

func agentsPage(page cli.Page, found subagent.Found) []string {
	rows := make([]cli.Row, len(found.Definitions))
	refused := 0
	for i, definition := range found.Definitions {
		rows[i] = cli.Row{Cells: []string{definition.Name, string(definition.Runs), agentSource(definition.Origin)}, Detail: definition.Description}
		switch definition.Runs {
		case subagent.RunsModel:
			rows[i].Mark, rows[i].Cells[1] = cli.Active, definition.Model
		case subagent.RunsInherit, subagent.RunsDisabled:
			rows[i].Mark = cli.Idle
		case subagent.RunsRefused:
			rows[i].Mark = cli.Fail
			refused++
		default:
			panic("tofu agents: unknown runs " + string(definition.Runs))
		}
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "all ready"}
	var counts []string
	if refused > 0 {
		verdict.Mark, counts = cli.Warn, append(counts, strconv.Itoa(refused)+" refused")
	}
	if len(found.Broken) > 0 {
		verdict.Mark, counts = cli.Fail, append(counts, strconv.Itoa(len(found.Broken))+" broken")
	}
	if len(counts) > 0 {
		verdict.Text = strings.Join(counts, " · ")
	}
	lines := append(page.Title("Sub-agents", []string{strconv.Itoa(len(found.Definitions)) + " defined"}, verdict), "")
	var facts []cli.Fact
	factsPerAgent := make([]int, len(found.Definitions))
	for i, definition := range found.Definitions {
		for _, fact := range agentFacts(definition) {
			if fact.Text != "" {
				facts, factsPerAgent[i] = append(facts, fact), factsPerAgent[i]+1
			}
		}
	}
	factLines := page.Facts(facts)
	for i, line := range page.Rows(rows) {
		lines = append(append(lines, cli.Indent(line)...), cli.Indent(cli.Indent(factLines[:factsPerAgent[i]]...)...)...)
		factLines = factLines[factsPerAgent[i]:]
	}
	if len(found.Broken) > 0 {
		broken := make([]cli.Row, len(found.Broken))
		for i, one := range found.Broken {
			broken[i] = cli.Row{Mark: cli.Fail, Cells: []string{page.Path(one.Path)}, Detail: one.Reason}
		}
		lines = append(append(lines, "", page.Section("broken", cli.Verdict{})), cli.Indent(page.Rows(broken)...)...)
	}
	for _, notice := range found.Notices {
		lines = append(lines, "", page.Glyph(cli.Warn)+" "+notice)
	}
	return lines
}

func agentFacts(definition subagent.Definition) []cli.Fact {
	facts := []cli.Fact{{Label: "tools", Text: strings.Join(definition.Tools, ", ")}}
	if definition.From != "file" {
		facts = append(facts, cli.Fact{Label: "from", Text: definition.From})
	}
	for _, reference := range definition.References {
		facts = append(facts, cli.Fact{Label: "reference", Text: reference.Name + cli.Gap + widget.Size(len(reference.Text))})
	}
	facts = append(facts, cli.Fact{Label: "cut", Text: strings.Join(definition.Cut, ", ")}, cli.Fact{Label: "ignores", Text: strings.Join(definition.IgnoredTools, ", ")})
	for _, reason := range definition.Refused {
		facts = append(facts, cli.Fact{Label: "refused", Text: reason})
	}
	for _, notice := range definition.Notices {
		facts = append(facts, cli.Fact{Label: "notice", Text: notice})
	}
	for _, shadowed := range definition.Shadowed {
		facts = append(facts, cli.Fact{Label: "shadows", Text: agentSource(shadowed.Origin)})
	}
	return facts
}
