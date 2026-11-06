package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"tofu/internal/konst"
)

type comparison struct {
	task, baseline string
	tools, other   outcome
	verdict        string
}

func tokens(bytes int) int {
	return (bytes + konst.SearchBytesPerToken - 1) / konst.SearchBytesPerToken
}

func (o outcome) spentTo(upTo int) (calls, bytes int, spent time.Duration) {
	for i, s := range o.steps {
		if upTo > 0 && i >= upTo {
			break
		}
		calls, bytes, spent = calls+1, bytes+s.bytes, spent+s.spent
	}
	return calls, bytes, spent
}

func (o outcome) toAnswer() string {
	if o.answeredAt == 0 {
		return "not reached"
	}
	calls, bytes, _ := o.spentTo(o.answeredAt)
	return fmt.Sprintf("%d tokens in %d calls", tokens(bytes), calls)
}

func render(rows []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/observe: http, db and search against bash on the server fixture, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Dev build: %s. go: %s. curl: %s. sqlite3: %s. ", host, began.Format("2006-01-02 15:04"), version(m.exe, "version"), version("go", "version"), version("curl", "--version"), version("sqlite3", "--version"))
	b.WriteString("Credential: none, no model call, no network beyond 127.0.0.1 (GOPROXY=off); in the fresh HOME no classifier is bound, so bash output is not sifted, only cut at the bash tool's own cap. ")
	b.WriteString("Every arm: one run, no warm-up, every call through tofu tool <name> <json> on the dev build, a fresh copy of bench/suites/tools/fixtures/server and a fresh HOME under " + m.scratch + "/<dir>. ")
	b.WriteString("The harness builds and starts the fixture server in each copy on a free port before the first call, the same for every arm, and those steps are not counted. ")
	fmt.Fprintf(&b, "Tokens: the call's stdout, what the model reads, in bytes over %d rounded up. Wall: the whole tofu tool process, its startup included; {port} in an input is the free port of that arm's server. ", konst.SearchBytesPerToken)
	b.WriteString("To the answer: tokens and calls summed up to the first call after which every answer pattern has appeared in some output; the cause on the total task is the symptom 650, the 25 cent row and the loop line for i := 1. Verdict: fewer tokens to the answer wins, an arm that never reaches it loses.\n\n")
	all := judged(rows)
	var wins, losses, ties []comparison
	for _, c := range all {
		switch {
		case strings.HasPrefix(c.verdict, "win"):
			wins = append(wins, c)
		case strings.HasPrefix(c.verdict, "loss"):
			losses = append(losses, c)
		default:
			ties = append(ties, c)
		}
	}
	b.WriteString("## tldr\n\n" + tldr(all, len(wins), len(losses), len(ties)) + "\n\n")
	source := "Source: tofu tool on the dev build, tools against bash, bench/suites/tools/observe, " + day + "."
	for _, side := range []struct {
		title string
		list  []comparison
	}{{"Wins", wins}, {"Losses", losses}, {"Ties", ties}} {
		b.WriteString("## " + side.title + "\n\n" + source + "\n\n")
		table(&b, []string{"Task", "Bash arm", "Tools to the answer", "Bash to the answer", "Verdict"}, cells(side.list, func(c comparison) []string {
			return []string{c.task, c.baseline, c.tools.toAnswer(), c.other.toAnswer(), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## Every arm\n\n" + source + "\n\n")
	table(&b, []string{"Task", "Arm", "Calls", "Tokens", "Wall", "To the answer", "Failed calls", "Failed"}, cells(rows, func(o outcome) []string {
		calls, bytes, spent := o.spentTo(0)
		failed := 0
		for _, s := range o.steps {
			if s.failed != "" {
				failed++
			}
		}
		return []string{o.task, o.arm, fmt.Sprint(calls), fmt.Sprint(tokens(bytes)), spent.Round(time.Millisecond).String(), o.toAnswer(), fmt.Sprint(failed), cell(o.failed)}
	}))
	b.WriteString("\n## Every call\n\n" + source + "\n\n")
	var calls [][]string
	for _, o := range rows {
		for i, s := range o.steps {
			calls = append(calls, []string{o.task, o.arm, fmt.Sprint(i + 1), s.tool, "`" + cell(s.args) + "`", fmt.Sprint(tokens(s.bytes)), s.spent.Round(time.Millisecond).String(), fmt.Sprint(s.reached), cell(s.failed)})
		}
	}
	table(&b, []string{"Task", "Arm", "Call", "Tool", "Input", "Tokens", "Wall", "Answer complete", "Failed"}, calls)
	return b.String()
}

func judged(rows []outcome) []comparison {
	var out []comparison
	for _, tools := range rows {
		if !tools.tools {
			continue
		}
		for _, other := range rows {
			if other.task != tools.task || other.tools {
				continue
			}
			c := comparison{task: tools.task, baseline: other.arm, tools: tools, other: other}
			_, toolBytes, _ := tools.spentTo(tools.answeredAt)
			_, otherBytes, _ := other.spentTo(other.answeredAt)
			switch {
			case tools.failed != "" || tools.answeredAt == 0:
				c.verdict = "loss: the tools arm did not reach the answer"
			case other.failed != "" || other.answeredAt == 0:
				c.verdict = "win: the bash arm did not reach the answer"
			case tokens(toolBytes) < tokens(otherBytes):
				c.verdict = fmt.Sprintf("win: %d tokens against %d", tokens(toolBytes), tokens(otherBytes))
			case tokens(toolBytes) > tokens(otherBytes):
				c.verdict = fmt.Sprintf("loss: %d tokens against %d", tokens(toolBytes), tokens(otherBytes))
			default:
				c.verdict = fmt.Sprintf("tie: %d tokens each", tokens(toolBytes))
			}
			out = append(out, c)
		}
	}
	return out
}

func tldr(all []comparison, wins, losses, ties int) string {
	var parts []string
	for _, c := range all {
		parts = append(parts, fmt.Sprintf("%s: tools %s, %s %s", c.task, c.tools.toAnswer(), c.baseline, c.other.toAnswer()))
	}
	return strings.Join(parts, ". ") + fmt.Sprintf(". The tools win %d comparisons, lose %d and tie %d.", wins, losses, ties)
}

func version(name string, args ...string) string {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return "not found"
	}
	for line := range strings.Lines(string(out)) {
		if after, ok := strings.CutPrefix(line, "commit"); ok {
			return strings.TrimSpace(after)
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

func cell(s string) string {
	if s == "" {
		return "n/a"
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

func cells[T any](list []T, row func(T) []string) [][]string {
	out := make([][]string, len(list))
	for i, one := range list {
		out[i] = row(one)
	}
	return out
}

func table(b *strings.Builder, header []string, rows [][]string) {
	if len(rows) == 0 {
		b.WriteString("none\n")
		return
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|" + strings.Repeat("---|", len(header)) + "\n")
	for _, cells := range rows {
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
}
