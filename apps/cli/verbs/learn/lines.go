package learn

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/internal/store/learn"
)

func scanLines(page cli.Page, run learn.Run, opts learnOpts, file string) []string {
	read := run.Read
	head := page.Subject("Learn") + page.Label(" · "+output.Plural(len(read.Sessions), "session")+" · "+run.Scope)
	if read.Typed > 0 {
		head += page.Label(" · " + read.From.Local().Format(time.DateOnly) + " to " + read.To.Local().Format(time.DateOnly))
	}
	how := "grouped by shared words only, with no model: the weaker mode; tofu settings set learn true groups by meaning"
	if run.Mode == learn.ModeModel {
		how = fmt.Sprintf("grouped by meaning by %s on your subscription, one request, %d tokens", run.Model, run.Sent.GroupTokens)
	}
	lines := []string{head, "  " + page.Label(how)}
	if run.Sent.Kept != "" {
		lines = append(lines, "  "+page.Label("the model did not group them, so the weaker grouping is shown: "+clip(run.Sent.Kept, learn.ShownTextRunes)))
	}
	lines = append(lines, "", page.Subject("What keeps going wrong"))
	for i, wrong := range run.Summary.Wrong {
		lines = append(lines, fmt.Sprintf("  %d %s", i+1, wrong))
	}
	if len(run.Summary.Wrong) == 0 {
		lines = append(lines, "  nothing said twice")
	}
	lines = append(lines, "  "+page.Label(fmt.Sprintf("it cost you %s, and the lead %s answering them", output.Plural(run.Summary.Repeats, "repeat"), output.Plural(run.Summary.Calls, "call"))))
	if len(run.Summary.Do) > 0 {
		lines = append(lines, "  "+page.Hint(strings.Join(run.Summary.Do, " · ")))
	}
	all := len(run.Findings) + len(run.Held)
	lines = append(lines, "", page.Subject("Findings")+page.Label(fmt.Sprintf(" · %d of %d, the cap is %d", len(run.Findings), all, learn.ProposalCap)))
	for _, f := range run.Findings {
		lines = append(lines, findingLines(page, f)...)
	}
	sections := []struct {
		name     string
		findings []learn.Finding
	}{{"Held back by the cap", run.Held}, {"Fixed in a later tofu", run.Fixed}, {"Already applied, rejected or drafted, until more sessions say it", run.Decided}, {"Watching, said once or in one place so far", run.Watching}, {"About your project, not tofu's business", run.Project}}
	for _, section := range sections {
		if len(section.findings) == 0 {
			continue
		}
		lines = append(lines, "", page.Subject(section.name)+page.Label(" · "+strconv.Itoa(len(section.findings))))
		shown := section.findings
		if !opts.all {
			shown = shown[:min(len(shown), learn.ProposalCap)]
		}
		for _, f := range shown {
			lines = append(lines, "  "+clip(f.Title, learn.ShownTextRunes), "    "+page.Label(briefOf(f)))
		}
	}
	jobs := run.Jobs
	if !opts.all {
		jobs = jobs[:min(len(jobs), learn.ProposalCap)]
	}
	lines = append(lines, "", page.Subject("Repeated jobs")+page.Label(fmt.Sprintf(" · %d of %d · the same tools for the same kind of request, dearest first", len(jobs), len(run.Jobs))))
	for i, job := range jobs {
		lines = append(lines, fmt.Sprintf("  %-2d %s", i+1, clip(job.Name, learn.ShownTextRunes)),
			"     "+page.Label(fmt.Sprintf("%s in %s · %.4f USD each · %.4f USD total · %d tokens each", output.Plural(job.Times, "time"), output.Plural(len(job.Sessions), "session"), job.CostEach, job.Cost, job.TokensEach)),
			"     "+page.Label("tools "+strings.Join(job.Tools, " > ")+" · from "+strings.Join(job.Sessions, ", ")))
	}
	if len(run.Jobs) == 0 {
		lines = append(lines, "  no job asked for twice and done with the same tools")
	}
	sent := "nothing, --local"
	switch {
	case run.Sent.Local && !opts.local:
		sent = "nothing: learn is off; tofu settings set learn true sends the windows"
	case !run.Sent.Local:
		sent = fmt.Sprintf("%d windows, %d bytes, %.6f USD, %d failed; labels uncalibrated, shown and not used", run.Sent.Windows, run.Sent.Bytes, run.Sent.Cost, run.Sent.Failed)
	}
	lines = append(lines, "", page.Subject("Read"),
		fmt.Sprintf("  %d of your messages, %d repeated prompts set aside · %d lead calls, %d sub-agent reports, %d forks", read.Typed, read.Repeated, read.Calls, read.Reports, read.Forks),
		"  to Jev  "+sent)
	if run.Sent.Dropped > 0 {
		lines = append(lines, fmt.Sprintf("  the model's answer: %d fields refused, such as %s", run.Sent.Dropped, strings.Join(run.Sent.Refused[:min(len(run.Sent.Refused), 2)], "; ")))
	}
	return append(lines, "  "+page.Label("run kept at ")+page.Path(file), page.Hint("tofu learn show <n> for the evidence · apply <n> · reject <n> --reason \"...\" · upstream <n>"))
}

func briefOf(f learn.Finding) string {
	brief := fmt.Sprintf("%s in %s · %s", output.Plural(f.Times, "time"), output.Plural(f.Sessions, "session"), f.Class.Label())
	if f.FixedIn != "" {
		brief += " · fixed in " + f.FixedIn + ", last seen on " + f.Built
		if f.FixedSameDay {
			brief += ", released the day it was last seen"
		}
	}
	if command := f.Command(); command != "" && f.ID > 0 {
		brief += " · " + command
	}
	return brief
}

func findingLines(page cli.Page, f learn.Finding) []string {
	lines := []string{fmt.Sprintf("  %-2d %s", f.ID, f.Title),
		"     " + page.Label(fmt.Sprintf("%s in %s · %s · %s confidence", output.Plural(f.Times, "time"), output.Plural(f.Sessions, "session"), f.Class.Label(), f.Confidence)),
		"     why   " + clip(f.Reason, learn.ShownTextRunes)}
	switch {
	case f.Target() == learn.TargetUpstream:
		lines = append(lines, "     fix   a report for tofu, written to a file and never sent")
	case f.Setting != "":
		lines = append(lines, "     fix   tofu settings set --scope project "+f.Setting+" "+f.Value)
	case f.Rule != "":
		lines = append(lines, "     fix   "+string(f.Target())+": "+clip(f.Rule, learn.ShownTextRunes))
	default:
		lines = append(lines, "     fix   no rule worded without a model")
	}
	if command := f.Command(); command != "" {
		lines = append(lines, "           "+page.Hint(command))
	}
	for _, q := range f.Quotes[:min(len(f.Quotes), learn.ShownQuotes)] {
		lines = append(lines, "     "+page.Label(fmt.Sprintf("%s %s %s", q.At.Local().Format(timeOfDay), q.Session, strconv.Quote(clip(q.Text, learn.ShownQuoteRunes)))))
	}
	return lines
}

func clip(text string, runes int) string {
	text = output.OneLine(text)
	if len([]rune(text)) <= runes {
		return text
	}
	return string([]rune(text)[:runes]) + "..."
}

func showLines(page cli.Page, run learn.Run, f learn.Finding) []string {
	lines := append([]string{page.Subject("Finding "+strconv.Itoa(f.ID)) + page.Label(" · run "+run.ID), "  " + f.Title}, findingLines(page, f)[1:3]...)
	if f.Rule != "" {
		lines = append(lines, "  rule: "+f.Rule)
	}
	if f.Retire != "" {
		lines = append(lines, "  "+page.Label("retires memory "+f.Retire+" in the "+string(f.Scope)+" scope"))
	}
	if f.Built != "" {
		lines = append(lines, "  "+page.Label("last seen on tofu "+f.Built+", from the day it was said and the release dates"))
	}
	lines = append(lines, "")
	for _, q := range f.Quotes {
		lines = append(lines, fmt.Sprintf("  %s %-18s %s", q.At.Local().Format(dayAndTime), q.Session, strconv.Quote(output.OneLine(q.Text))))
		at := slices.IndexFunc(run.Said, func(s learn.Said) bool { return s.At.Equal(q.At) && s.Session == q.Session })
		if at >= 0 && at < len(run.Labels) {
			label := run.Labels[at]
			lines = append(lines, page.Label(fmt.Sprintf("    jev, uncalibrated: %v %v %s", label.Answers, label.Chosen, label.Failure)))
		}
	}
	for _, c := range f.Checks {
		held := page.Glyph(cli.Fail) + " absent"
		if c.Present {
			held = page.Glyph(cli.Done) + " present"
		}
		lines = append(lines, fmt.Sprintf("  %s when it recurred at %s in %s, request %s", held, c.At.Local().Format(timeOfDay), c.Session, c.Request))
	}
	return lines
}
