package rules

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/platform/text"
	"tofu/internal/project/boardy"
)

const showUsage = "tofu rules show <id> [--library dir] [--dir project] [--json]"

type ShowReport struct {
	ID       string                `json:"id"`
	Kind     string                `json:"kind"`
	Domain   string                `json:"domain"`
	Concern  string                `json:"concern"`
	Mode     string                `json:"mode"`
	Origin   string                `json:"origin"`
	File     string                `json:"file"`
	Text     string                `json:"text"`
	Notes    string                `json:"notes,omitempty"`
	Override *host.OverrideListing `json:"override,omitempty"`
}

func showVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules show", UsageLine: showUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseFlags(args)
	switch {
	case err != nil:
	case len(opts.rest) == 0:
		err = errors.New("no rule id")
	case len(opts.rest) > 1:
		err = fmt.Errorf("unknown argument %q", opts.rest[1])
	}
	if err != nil {
		return o.Usage(err)
	}
	boards, opened := boardy.OpenStore(opts.dir)
	stack, err := wiring.StackRules(opts.library, boards, opened)
	if err != nil {
		return o.Fail(err)
	}
	layers, err := wiring.UserRuleLayers(opts.dir)
	if err != nil {
		return o.Fail(err)
	}
	id, known := opts.rest[0], slices.Concat(stack.Rules, stack.ShippedOff)
	at := slices.IndexFunc(known, func(r rules.Rule) bool { return r.ID == id })
	if at < 0 {
		return o.Fail(output.ProblemError{What: fmt.Sprintf("no rule %q", id), Hint: "closest: " + strings.Join(closestIDs(id, known), ", ")})
	}
	r := known[at]
	report := ShowReport{ID: r.ID, Kind: string(r.Kind), Domain: r.Domain, Concern: string(r.Concern), Mode: string(r.Mode), Origin: ruleOrigin(stack, layers, r), File: r.File, Text: r.Text, Notes: r.Notes, Override: stack.Applied(r)}
	return o.Done(true, report, report.lines)
}

func closestIDs(id string, known []rules.Rule) []string {
	ids := make([]string, len(known))
	for i, r := range known {
		ids[i] = r.ID
	}
	slices.SortStableFunc(ids, func(a, b string) int { return cmp.Compare(text.EditDistance(id, a), text.EditDistance(id, b)) })
	far := slices.IndexFunc(ids, func(candidate string) bool {
		return text.EditDistance(id, candidate) > len(id)/konst.NearestWordBytesPerEdit
	})
	if far < 0 {
		return ids
	}
	return ids[:max(far, 1)]
}

func (report ShowReport) lines(page cli.Page) []string {
	lines := append(page.Title("Rule "+report.ID, []string{report.Kind}, cli.Verdict{Mark: listingMark(rules.Mode(report.Mode)), Text: cmp.Or(report.Mode, "on")}), "")
	facts := []cli.Fact{
		{Label: "domain", Text: report.Domain},
		{Label: "concern", Text: report.Concern},
		{Label: "from", Text: report.Origin + output.FactSeparator + page.Path(report.File)},
	}
	if report.Override != nil {
		facts = append(facts, cli.Fact{Label: "override", Text: wiring.OverrideWhy(*report.Override)})
	}
	lines = append(append(lines, cli.Indent(page.Facts(facts)...)...), "", page.Section("text", cli.Verdict{}))
	lines = append(lines, cli.Indent(report.Text)...)
	if report.Notes != "" {
		lines = append(append(lines, "", page.Section("notes", cli.Verdict{})), cli.Indent(report.Notes)...)
	}
	return lines
}
