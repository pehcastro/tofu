package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"tofu/interface/cli"
	"tofu/internal/hook"
	"tofu/internal/shell"
)

const hooksSubcommands = "tofu hooks [trust] [--json]"

const (
	sessionEndOther = "other"
	sessionEndExit  = "prompt_input_exit"
)

func hooksVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "hooks", usageLine: hooksSubcommands, asJSON: jsonAsked(args), out: out, errOut: errOut}
	words := withoutJSON(args)
	engine := hook.Load(".", shell.Choice{})
	switch {
	case len(words) == 0:
		return hooksList(o, engine)
	case len(words) == 1 && words[0] == "trust":
		o.verb = "hooks trust"
		return hooksTrust(o, engine)
	case len(words) == 1:
		return o.usage(fmt.Errorf("there is no subcommand %q", words[0]))
	}
	return o.usage(errors.New("too many arguments"))
}

func hooksList(o verbOutput, engine *hook.Engine) int {
	hooks, problems := engine.Hooks(), engine.Problems()
	project, _ := filepath.Abs(".")
	return o.done(true, struct {
		Hooks    []hook.Hook `json:"hooks"`
		Problems []string    `json:"problems"`
	}{append([]hook.Hook{}, hooks...), append([]string{}, problems...)}, func(page cli.Page) []string {
		if len(hooks) == 0 && len(problems) == 0 {
			return page.Title("Hooks", nil, cli.Verdict{Mark: cli.Idle, Text: "none in this project or your home"})
		}
		untrusted, skipped := 0, 0
		rows, under := make([]cli.Row, len(hooks)), make([]string, len(hooks))
		for i, one := range hooks {
			mark, state := cli.Done, string(one.Trust)
			switch {
			case one.Skipped != "":
				mark, state, skipped = cli.Idle, "skipped", skipped+1
			case one.Trust == hook.TrustNew || one.Trust == hook.TrustChanged:
				mark, untrusted = cli.Warn, untrusted+1
			case one.Trust == hook.TrustRefused:
				mark = cli.Fail
			}
			rows[i] = cli.Row{Mark: mark, Cells: []string{string(one.Event), cmp.Or(one.Matcher, "*"), state}, Detail: one.Command}
			under[i] = relativeToRoot(project, page.Path(one.File)) + " · " + string(one.Level) + " · " + cmp.Or(one.Skipped, lastResult(one.Last))
		}
		facts := []string{countOf(len(hooks), "hook")}
		if untrusted > 0 {
			facts = append(facts, strconv.Itoa(untrusted)+" not trusted")
		}
		if skipped > 0 {
			facts = append(facts, strconv.Itoa(skipped)+" skipped")
		}
		lines := append(page.Title("Hooks", facts, cli.Verdict{}), "")
		for i, line := range page.Rows(rows) {
			lines = append(lines, cli.Indent(line, "  "+page.Label(under[i]))...)
		}
		for _, problem := range problems {
			lines = append(lines, cli.Indent(page.Glyph(cli.Warn)+" "+problem)...)
		}
		if untrusted > 0 {
			lines = append(lines, "", page.Hint("tofu hooks trust"))
		}
		return lines
	})
}

func lastResult(last *hook.Result) string {
	switch {
	case last == nil:
		return "not run yet"
	case last.Problem != "":
		return "last: " + last.Problem
	}
	said := "last: exit " + strconv.Itoa(last.Exit) + " in " + strconv.FormatInt(last.DurationMS, 10) + " ms at " + last.At.Format("2006-01-02 15:04")
	if last.Said != "" {
		said += ", said " + strconv.Quote(last.Said)
	}
	return said
}

func hooksTrust(o verbOutput, engine *hook.Engine) int {
	untrusted := engine.Untrusted()
	if err := engine.Answer(untrusted, hook.AnswerAlways); err != nil {
		return o.fail(err)
	}
	return o.done(true, struct {
		Trusted []hook.Hook `json:"trusted"`
	}{append([]hook.Hook{}, untrusted...)}, func(page cli.Page) []string {
		if len(untrusted) == 0 {
			return []string{page.Glyph(cli.Idle) + " every project hook here is already trusted, skipped or refused"}
		}
		lines := make([]string, len(untrusted))
		for i, one := range untrusted {
			lines[i] = page.Receipt(cli.Added, "trusted "+string(one.Event)+" "+cmp.Or(one.Matcher, "*")+": "+one.Command, one.File)
		}
		return lines
	})
}
