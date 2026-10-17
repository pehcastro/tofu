package main

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/widget"
)

func traceMark(row traceRow) cli.Mark {
	switch row.Outcome {
	case traceFailed:
		return cli.Fail
	case traceStopped:
		return cli.Warn
	}
	return cli.Idle
}

func traceRowLines(page cli.Page, timeline traceTimeline, wide bool) []string {
	facts := []string{timeline.Handle, plural(timeline.Matched, "matching row")}
	if timeline.Matched > len(timeline.Rows) {
		facts = append(facts, "the last "+strconv.Itoa(len(timeline.Rows)))
	}
	lines := append(page.Title("Trace", facts, cli.Verdict{}), "")
	if len(timeline.Rows) == 0 {
		return append(lines, cli.Indent(page.Label("nothing matched"))...)
	}
	rows := make([]cli.Row, len(timeline.Rows))
	for i, row := range timeline.Rows {
		rows[i] = cli.Row{Mark: traceMark(row), Cells: []string{row.At.Format(time.TimeOnly), string(row.Kind), row.who()}, Detail: recordedIn(row.RecordedIn) + row.Text}
		if wide {
			rows[i].Detail = traced(rows[i].Detail, row.Turn, row.ID)
		}
	}
	if wide {
		return append(lines, cli.Indent(page.Rows(rows)...)...)
	}
	return append(lines, fitted(page, rows)...)
}

func fitted(page cli.Page, rows []cli.Row) []string {
	lines := cli.Indent(page.Rows(rows)...)
	for i := range lines {
		lines[i] = widget.Fit(lines[i], page.Width)
	}
	return lines
}

func traceSummaryLines(page cli.Page, timeline traceTimeline) []string {
	rows := timeline.Rows
	facts := []string{timeline.Handle, plural(timeline.Events, "event")}
	if len(rows) > 0 {
		last := rows[len(rows)-1].At
		facts = append(facts, timeline.began.Format(time.TimeOnly)+" to "+last.Format(time.TimeOnly)+" ("+widget.Until(last.Sub(timeline.began))+")")
	}
	kinds, ended, failures, stops := map[traceKind]int{}, map[string]int{}, map[traceKind][]traceRow{}, map[string][]traceRow{}
	began, startup := 0, ""
	for _, row := range rows {
		kinds[row.Kind]++
		switch {
		case row.Kind == kindTurn && row.Agent == "" && row.State == "began":
			began++
		case row.Kind == kindTurn && row.Agent == "" && row.Outcome == traceStopped:
			ended[string(traceStopped)]++
		case row.Kind == kindTurn && row.Agent == "" && (row.State == session.OutcomeStopped.String() || row.State == session.OutcomeUnset.String()):
			ended["finished"]++
		case row.Kind == kindTurn && row.Agent == "":
			ended[row.State]++
		case row.Kind == kindStartup:
			total, _, _ := strings.Cut(strings.TrimPrefix(row.Text, "startup took "), ":")
			startup = row.At.Format(time.TimeOnly) + " " + total + ", --kind startup --wide for each step"
		}
		switch row.Outcome {
		case traceFailed:
			failures[row.Kind] = append(failures[row.Kind], row)
		case traceStopped:
			stops[row.Turn] = append(stops[row.Turn], row)
		}
	}
	turnWords, open := []string{plural(began, "lead turn")}, began
	for _, state := range slices.Sorted(maps.Keys(ended)) {
		turnWords, open = append(turnWords, strconv.Itoa(ended[state])+" "+state), open-ended[state]
	}
	if open > 0 {
		turnWords = append(turnWords, strconv.Itoa(open)+" open")
	}
	var kindWords []string
	for _, kind := range traceKinds() {
		if kinds[kind] > 0 {
			kindWords = append(kindWords, strconv.Itoa(kinds[kind])+" "+string(kind))
		}
	}
	lines := append(page.Title("Trace", facts, cli.Verdict{}), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "turns", Text: strings.Join(turnWords, ", ")},
		{Label: "rows", Text: strings.Join(kindWords, " · ")}, {Label: "startup", Text: startup}})...)...)
	section := func(name string, rows []cli.Row) {
		if len(rows) > 0 {
			lines = append(append(lines, "", page.Section(name, cli.Verdict{})), fitted(page, rows)...)
		}
	}
	section("sub-agents", traceAgentRows(timeline))
	var failed []cli.Row
	for _, kind := range traceKinds() {
		if group := failures[kind]; len(group) > 0 {
			latest := group[len(group)-1]
			failed = append(failed, cli.Row{Mark: cli.Fail, Cells: []string{string(kind), plural(len(group), "failure"), "last " + latest.At.Format(time.TimeOnly), latest.who()}, Detail: latest.Text})
		}
	}
	section("errors", failed)
	section("stops", traceStopRows(stops))
	hint := "tofu session trace " + timeline.Handle
	return append(append(lines, "", page.Section("drill down", cli.Verdict{})), cli.Indent(
		page.Hint(hint+" --kind stop,error"), page.Hint(hint+" --since 1h --agent lead --kind message"),
		page.Hint(hint+" --turn <id> --last 20"), page.Hint(hint+" --grep <text> --wide"), page.Hint(hint+" --full"))...)
}

func traceAgentRows(timeline traceTimeline) []cli.Row {
	var agents []cli.Row
	idle := map[string][]string{}
	for _, run := range timeline.agents {
		calls, last, said := 0, run.StartedAt, ""
		for _, row := range timeline.Rows {
			if row.Agent != run.Agent {
				continue
			}
			last = row.At
			if row.Kind == kindCall || row.Kind == kindShell || row.Kind == kindCron {
				calls++
			}
			if row.Kind == kindReport || row.Outcome == traceStopped {
				said = row.Text
			}
		}
		if calls == 0 {
			idle[run.Status] = append(idle[run.Status], run.Agent)
			continue
		}
		mark := map[string]cli.Mark{subagent.Finished.String(): cli.Done, subagent.Working.String(): cli.Active, subagent.Parked.String(): cli.Warn, subagent.Errored.String(): cli.Fail}[run.Status]
		agents = append(agents, cli.Row{Mark: cmp.Or(mark, cli.Idle), Cells: []string{run.Agent, cmp.Or(run.Definition, "unnamed"), run.Status, plural(calls, "call"), "last " + last.Format(time.TimeOnly)}, Detail: said})
	}
	for _, status := range slices.Sorted(maps.Keys(idle)) {
		agents = append(agents, cli.Row{Mark: cli.Idle, Cells: []string{plural(len(idle[status]), "agent"), "no calls here", status}, Detail: strings.Join(idle[status], ", ")})
	}
	return agents
}

func traceStopRows(stops map[string][]traceRow) []cli.Row {
	turns := slices.Collect(maps.Keys(stops))
	slices.SortFunc(turns, func(a, b string) int { return stops[a][0].At.Compare(stops[b][0].At) })
	stopped := make([]cli.Row, len(turns))
	for i, turnID := range turns {
		var who []string
		for _, row := range stops[turnID] {
			if !slices.Contains(who, row.who()) {
				who = append(who, row.who())
			}
		}
		stopped[i] = cli.Row{Mark: cli.Warn, Cells: []string{stops[turnID][0].At.Format(time.TimeOnly), cmp.Or(turnID, "no turn")}, Detail: "stopped " + strings.Join(who, ", ") + " · who asked is not recorded"}
	}
	return stopped
}
