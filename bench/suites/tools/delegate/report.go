package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
)

type comparison struct {
	mine, other outcome
	verdict     string
}

func render(rows []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/delegate: race against one sub-agent, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s, %s/%s, %d logical CPUs. Date: %s. Dev build: %s. claude: %s. go: %s. ", host, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), began.Format("2006-01-02 15:04"), version(m.exe, "version"), version("claude", "--version"), version("go", "version"))
	fmt.Fprintf(&b, "Credential: claude-sub subscription for every tofu arm, a copy of the owner's login in a fresh HOME per arm, orchestrator and sub-agent bound to %s, classifier binding copied from the owner's home for the gate; the claude arm on the owner's own Claude login. No API key: CLAUDE*, ANTHROPIC*, OPENROUTER* and *_API_KEY are dropped from every arm's environment. ", m.model)
	b.WriteString("Every arm: one run, no warm-up, a fresh copy of bench/suites/tools/fixtures/go with a one-commit git history under " + m.scratch + "/<dir>. ")
	b.WriteString("Passed: go test -count=1 over every package but broken/ passes on the copy after the arm. Tokens in: prompt plus cache read plus cache write over every request. Files changed: git status entries after the arm. Wall: the arm's process, start to exit.\n\n")
	b.WriteString("## Cost per arm, stated before the run\n\n")
	for _, line := range costs(m.model) {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\nThe off arm is one sub-agent on one brief with no race, since that is what race replaces: the same entrant, the same brief, the same check, one at a time.\n\n")
	all := judged(rows)
	b.WriteString("## tldr\n\n" + tldr(rows, all) + "\n\n")
	for _, s := range []struct {
		side   side
		source string
	}{{sideTool, "Source: tofu tool race and tofu run --no-subagents on the dev build, tool against tool, " + day + "."}, {sideTurn, "Source: tofu run and claude -p, turn against turn, " + day + "."}} {
		for _, kind := range []struct{ title, verdict string }{{"Wins", "win:"}, {"Losses", "loss:"}, {"Ties", "tie:"}} {
			fmt.Fprintf(&b, "## %s, %s against %s\n\n%s\n\n", kind.title, s.side, s.side, s.source)
			var list []comparison
			for _, c := range all {
				if c.mine.arm.side == s.side && strings.HasPrefix(c.verdict, kind.verdict) {
					list = append(list, c)
				}
			}
			table(&b, []string{"Arm", "Against", "Passed", "Other passed", "Tokens", "Other tokens", "Wall", "Other wall", "Verdict"}, cells(list, func(c comparison) []string {
				return []string{c.mine.arm.name, c.other.arm.name, passRate(c.mine), passRate(c.other), fmt.Sprint(c.mine.tokens()), fmt.Sprint(c.other.tokens()), c.mine.spent.Round(time.Second).String(), c.other.spent.Round(time.Second).String(), c.verdict}
			}))
			b.WriteString("\n")
		}
	}
	b.WriteString("## Every arm\n\nSource: every arm of bench/suites/tools/delegate on the dev build, " + day + ".\n\n")
	table(&b, []string{"Side", "Arm", "Passed", "Check after", "Files changed", "Requests", "Tokens in", "Tokens out", "Wall", "Model", "Spend", "Note", "Failed"}, cells(rows, func(o outcome) []string {
		return []string{string(o.arm.side), o.arm.name, passRate(o), cell(o.check), fmt.Sprint(o.changed), fmt.Sprint(o.requests), fmt.Sprint(o.in), fmt.Sprint(o.out), o.spent.Round(time.Second).String(), cell(o.model), cell(o.spend), cell(o.note), cell(o.failed)}
	}))
	return b.String()
}

func judged(rows []outcome) []comparison {
	pairs := map[string][]string{armRace: {armOneSmall, armOneTiers}, armTofuTurn: {armClaudeTurn}}
	var out []comparison
	for _, mine := range rows {
		for _, other := range rows {
			if !slices.Contains(pairs[mine.arm.name], other.arm.name) {
				continue
			}
			c := comparison{mine: mine, other: other}
			spent, otherSpent := mine.tokens(), other.tokens()
			switch {
			case mine.requests == 0:
				c.verdict = "loss: the arm made no request"
			case other.requests == 0:
				c.verdict = "tie: the other arm made no request"
			case mine.passed && !other.passed:
				c.verdict = "win: passes where the other fails"
			case !mine.passed && other.passed:
				c.verdict = "loss: fails where the other passes"
			case spent < otherSpent:
				c.verdict = fmt.Sprintf("win: same result on %d tokens against %d", spent, otherSpent)
			case spent > otherSpent:
				c.verdict = fmt.Sprintf("loss: same result on %d tokens against %d", spent, otherSpent)
			default:
				c.verdict = "tie: same result, same tokens"
			}
			out = append(out, c)
		}
	}
	return out
}

func tldr(rows []outcome, all []comparison) string {
	var parts []string
	for _, o := range rows {
		parts = append(parts, fmt.Sprintf("%s passed %s on %d tokens in %s", o.arm.name, passRate(o), o.tokens(), o.spent.Round(time.Second)))
	}
	count := map[string]int{}
	for _, c := range all {
		verdict, _, _ := strings.Cut(c.verdict, ":")
		count[verdict]++
	}
	return strings.Join(parts, "; ") + fmt.Sprintf(". Over the comparisons: %d wins, %d losses, %d ties.", count["win"], count["loss"], count["tie"])
}

func (o outcome) tokens() int { return o.in + o.out }

func passRate(o outcome) string {
	if o.passed {
		return "1 of 1"
	}
	return "0 of 1"
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
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "/"), "\n", " ")
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
