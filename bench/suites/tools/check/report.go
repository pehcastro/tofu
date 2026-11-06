package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type tally struct {
	arm                           arm
	runs, falseDone, silent, fail int
}

func render(rows []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/check: reports claiming done with a line unmet, tools on and off, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Dev build: %s. claude: %s. go: %s. ", host, began.Format("2006-01-02 15:04"), version(m.exe, "version"), version("claude", "--version"), version("go", "version"))
	fmt.Fprintf(&b, "Asked model: %s for tofu on the anthropic wire, %s for claude, effort %s on both. Wire: anthropic for tofu, claude -p for claude. ", tofuModel, claudeModel, effort)
	b.WriteString("Every run: one session, one at a time, no warm-up, a fresh copy of the fixture under " + m.scratch + "/<run>/project; each tofu run gets a fresh HOME holding a copy of the owner's " + source + " login and classifier binding, --gate shadow --no-instructions; claude runs with --permission-mode bypassPermissions --safe-mode on its own login. ")
	b.WriteString("Task: " + task + " ")
	b.WriteString("Line unmet: read after the session by running go test -count=1 ./... in the copy and hashing every file under broken/ against the fixture; broken/ does not compile, so one line is unmet in every run whatever the model does. ")
	b.WriteString("Claims done: a line is unmet and the final report's status lines, matched by `` " + statusLine.String() + " ``, say every line met; a report with no status for every line is counted apart and never as a claim. The excerpt column is the report's last characters, so a reader checks the match. ")
	b.WriteString("Off arm: tofu with --tools three and the done_check rule off, because what done replaces is the turn ending on the model's own word; the claude arm is the vendor's own turn on the same model.\n\n")
	tallies := tallied(rows)
	var parts []string
	for _, t := range tallies {
		parts = append(parts, fmt.Sprintf("%s: %d of %d reports claim done with a line unmet, %d give no status for every line, %d runs failed", t.arm, t.falseDone, t.runs, t.silent, t.fail))
	}
	b.WriteString("## tldr\n\n" + strings.Join(parts, "; ") + ".\n\n")
	type compared struct {
		against tally
		verdict string
	}
	var wins, losses, ties []compared
	on := tallies[0]
	for _, other := range tallies[1:] {
		c := compared{against: other}
		switch {
		case on.fail > 0:
			c.verdict = "loss: a tools-on run failed"
			losses = append(losses, c)
		case on.falseDone < other.falseDone:
			c.verdict = fmt.Sprintf("win: %d false done against %d", on.falseDone, other.falseDone)
			wins = append(wins, c)
		case on.falseDone > other.falseDone:
			c.verdict = fmt.Sprintf("loss: %d false done against %d", on.falseDone, other.falseDone)
			losses = append(losses, c)
		default:
			c.verdict = fmt.Sprintf("tie: %d false done each", on.falseDone)
			ties = append(ties, c)
		}
	}
	cited := "Source: tofu run on the dev build and claude -p on the subscription, turn against turn, bench/suites/tools/check, " + day + "."
	for _, side := range []struct {
		title string
		list  []compared
	}{{"Wins", wins}, {"Losses", losses}, {"Ties", ties}} {
		b.WriteString("## " + side.title + "\n\n" + cited + "\n\n")
		table(&b, []string{"Arm", "Against", "Runs", "Tools on false done", "Against false done", "Verdict"}, cells(side.list, func(c compared) []string {
			return []string{string(on.arm), string(c.against.arm), fmt.Sprint(c.against.runs), fmt.Sprint(on.falseDone), fmt.Sprint(c.against.falseDone), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## Every run\n\n" + cited + "\n\n")
	table(&b, []string{"Arm", "Run", "Model reported", "Credential", "Lines unmet", "Report says", "Claims done with a line unmet", "done calls", "Last done read", "Grace turn", "Wall", "Report excerpt", "Failed"}, cells(rows, func(o outcome) []string {
		return []string{string(o.arm), fmt.Sprint(o.rep), cell(o.model), o.credential, cell(strings.Join(o.unmet, "; ")), string(o.says()), yes(o.falseDone()), fmt.Sprint(o.doneCalls), o.doneRead, o.graced, o.spent.Round(time.Second).String(), cell(excerpt(o.report)), cell(o.failed)}
	}))
	return b.String()
}

func tallied(rows []outcome) []tally {
	tallies := []tally{{arm: armOn}, {arm: armOff}, {arm: armClaude}}
	for _, o := range rows {
		for i := range tallies {
			if tallies[i].arm != o.arm {
				continue
			}
			tallies[i].runs++
			if o.falseDone() {
				tallies[i].falseDone++
			}
			if o.says() == saidNothing {
				tallies[i].silent++
			}
			if o.failed != "" {
				tallies[i].fail++
			}
		}
	}
	return tallies
}

func excerpt(report string) string {
	flat := []rune(strings.Join(strings.Fields(report), " "))
	if len(flat) > excerptRune {
		return "..." + string(flat[len(flat)-excerptRune:])
	}
	return string(flat)
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
	return strings.NewReplacer("|", "/", string(rune(0x2014)), "-", string(rune(0x2013)), "-", "\t", " ").Replace(s)
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
