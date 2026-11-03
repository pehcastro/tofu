package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/internal/store/session"
)

const (
	rulesWhyUsage      = "tofu rules why <session> [turn] [--json]"
	ruleWhyFired       = "fired"
	ruleWhySwitchedOff = "switched off"
)

type ruleWhyRow struct {
	Turn       string `json:"turn"`
	Agent      string `json:"agent"`
	Rule       string `json:"rule"`
	State      string `json:"state"`
	ReasonKind string `json:"reason_kind"`
	Mode       string `json:"mode"`
	Why        string `json:"why,omitempty"`
	File       string `json:"file,omitempty"`
}

type rulesWhyReport struct {
	Session string       `json:"session"`
	Turns   []string     `json:"turns"`
	Rows    []ruleWhyRow `json:"rows"`
}

func (r ruleWhyRow) group() string { return r.Turn + "\x00" + r.Agent }

func ruleWhyState(part session.PromptPart) string {
	switch {
	case part.Off:
		return ruleWhySwitchedOff
	case part.Sent():
		return ruleWhyFired
	}
	return string(part.State)
}

func rulesWhyVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules why", UsageLine: rulesWhyUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	named := output.WithoutJSON(args)
	if len(named) == 0 || len(named) > 2 || slices.ContainsFunc(named, func(arg string) bool { return strings.HasPrefix(arg, "-") }) {
		return o.Usage(errors.New("name a session, and a turn of it to see one turn"))
	}
	store, err := session.Open()
	if err != nil {
		return o.Fail(err)
	}
	store = store.ReadOnce()
	header, err := sessionHeader(store, named[0])
	if err != nil {
		return o.Fail(err)
	}
	events, err := store.Events(header.ID)
	if err != nil {
		return o.Fail(err)
	}
	report, placed, groups := rulesWhyReport{Session: handleOf(store, header.ID)}, map[string]int{}, []string{}
	for _, event := range events {
		if event.Kind != session.EventPrompt || header.CarriedFrom != nil && event.Turn == header.ID {
			continue
		}
		if !slices.Contains(report.Turns, event.Turn) {
			report.Turns = append(report.Turns, event.Turn)
		}
		var prompt session.PromptBody
		_ = json.Unmarshal(event.Body, &prompt)
		for _, part := range prompt.Parts {
			if part.Rule == "" {
				continue
			}
			row := ruleWhyRow{Turn: event.Turn, Agent: cmp.Or(event.Agent, traceLead), Rule: part.Rule, State: ruleWhyState(part), ReasonKind: part.Reason, Mode: part.Mode, Why: output.OneLine(part.Why), File: part.File}
			if !slices.Contains(groups, row.group()) {
				groups = append(groups, row.group())
			}
			key := row.group() + "\x00" + row.Rule
			if at, seen := placed[key]; seen {
				report.Rows[at] = row
				continue
			}
			placed[key] = len(report.Rows)
			report.Rows = append(report.Rows, row)
		}
	}
	slices.SortStableFunc(report.Rows, func(a, b ruleWhyRow) int {
		return cmp.Compare(slices.Index(groups, a.group()), slices.Index(groups, b.group()))
	})
	if len(named) == 2 {
		if !slices.Contains(report.Turns, named[1]) {
			return o.Refuse(output.ExitUsage, cli.Problem{What: report.Session + " holds no turn " + named[1], Hint: "its turns: " + strings.Join(report.Turns, ", ")})
		}
		report.Turns = named[1:]
		report.Rows = slices.DeleteFunc(report.Rows, func(row ruleWhyRow) bool { return row.Turn != named[1] })
	}
	return o.Done(true, report, func(page cli.Page) []string { return rulesWhyLines(page, report) })
}

func rulesWhyLines(page cli.Page, report rulesWhyReport) []string {
	counts := map[string]int{}
	for _, row := range report.Rows {
		counts[row.State]++
	}
	var said []string
	for _, state := range []string{ruleWhyFired, string(session.PartHeld), string(session.PartDropped), ruleWhySwitchedOff} {
		if counts[state] > 0 {
			said = append(said, strconv.Itoa(counts[state])+" "+state)
		}
	}
	lines := page.Title("Rules why", []string{report.Session, output.Plural(len(report.Turns), "turn")}, cli.Verdict{Text: cmp.Or(strings.Join(said, ", "), "no rule is recorded")})
	var rows []cli.Row
	for i, row := range report.Rows {
		mark := map[string]cli.Mark{ruleWhyFired: cli.Done, ruleWhySwitchedOff: cli.Removed}[row.State]
		rows = append(rows, cli.Row{Mark: cmp.Or(mark, cli.Idle), Cells: []string{row.Rule}, Detail: traced(row.State, cmp.Or(row.ReasonKind, "reason kind not recorded"), row.Mode, row.Why)})
		if i+1 < len(report.Rows) && report.Rows[i+1].group() == row.group() {
			continue
		}
		lines = append(append(lines, "", page.Section(traced(row.Turn, row.Agent, output.Plural(len(rows), "rule")), cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
		rows = nil
	}
	return lines
}
