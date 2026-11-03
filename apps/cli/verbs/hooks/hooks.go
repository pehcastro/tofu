package hooks

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/internal/agent/hooks"
	"tofu/internal/host"
	"tofu/internal/tools/shell/process"
)

const Subcommands = "tofu hooks [trust] [--json]"

func Verb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "hooks", UsageLine: Subcommands, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	words := output.WithoutJSON(args)
	engine := hook.Load(".", shell.Choice{})
	switch {
	case len(words) == 0:
		return hooksList(o, engine)
	case len(words) == 1 && words[0] == "trust":
		o.Name = "hooks trust"
		return hooksTrust(o, engine)
	case len(words) == 1:
		return o.Usage(fmt.Errorf("there is no subcommand %q", words[0]))
	}
	return o.Usage(errors.New("too many arguments"))
}

func hooksList(o output.Verb, engine *hook.Engine) int {
	hooks, problems := engine.Hooks(), engine.Problems()
	project, _ := filepath.Abs(".")
	return o.Done(true, host.HooksReport{Hooks: append([]hook.Hook{}, hooks...), Problems: append([]string{}, problems...)}, func(page cli.Page) []string {
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
			under[i] = output.RelativeToRoot(project, page.Path(one.File)) + " · " + string(one.Level) + " · " + cmp.Or(one.Skipped, lastResult(one.Last))
		}
		facts := []string{output.Plural(len(hooks), "hook")}
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

func hooksTrust(o output.Verb, engine *hook.Engine) int {
	untrusted := engine.Untrusted()
	if err := engine.Answer(untrusted, hook.AnswerAlways); err != nil {
		return o.Fail(err)
	}
	return o.Done(true, host.HooksTrusted{Trusted: append([]hook.Hook{}, untrusted...)}, func(page cli.Page) []string {
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
