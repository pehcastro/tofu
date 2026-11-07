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
	tool, lines outcome
	verdict     string
}

func render(rows []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/debug: debug against print lines, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Dev build: %s. ", host, began.Format("2006-01-02 15:04"), version(m.exe, "version"))
	for _, tool := range [][]string{{"dlv", "dlv", "version"}, {"go", "go", "version"}, {"python", "python", "--version"}, {"debugpy", "python", "-c", "import debugpy; print(debugpy.__version__)"}, {"rtk", "rtk", "--version"}} {
		fmt.Fprintf(&b, "%s: %s. ", tool[0], version(tool[1], tool[2:]...))
	}
	b.WriteString("Credential: none, no model call, no network (GOPROXY=off). Every arm: one run, no warm-up, a fresh copy of the fixture and a fresh HOME under " + m.scratch + "/<dir>. The bench writes a Till into the copy, never into the fixture: Ring is right on calls 1, 2 and 4 and takes ten percent off call 3, so the test sees 2425 for 2500. The job: show call 3 and its value, 675 where 750 was due. ")
	b.WriteString("Off arm: print lines, the thing debug replaces, by a fixed procedure: edit a print in (and the fmt import in go), run the test once, read it, edit both out. It runs twice, the test through tofu tool bash and the test run raw, because tofu bash runs a command through the rtk proxy named in library/tools/shell/proxy.yaml, which summarizes test output. ")
	fmt.Fprintf(&b, "Calls: tofu tool or shell commands, each counted once. Tokens: output bytes of every call over %d rounded up. Written: source lines the tree held beyond the start when the answer was read. Left: source lines beyond the start after the arm ended. Files off: files that differ from the start after the arm.\n\n", konst.SearchBytesPerToken)
	var wins, losses, ties []comparison
	for _, c := range judged(rows) {
		switch {
		case strings.HasPrefix(c.verdict, "win"):
			wins = append(wins, c)
		case strings.HasPrefix(c.verdict, "loss"):
			losses = append(losses, c)
		default:
			ties = append(ties, c)
		}
	}
	b.WriteString("## tldr\n\n" + tldr(rows) + fmt.Sprintf(" debug wins %d comparisons, loses %d and ties %d.\n\n", len(wins), len(losses), len(ties)))
	source := "Source: tofu tool debug on the dev build against print lines by a fixed procedure, bench/suites/tools/debug, " + day + "."
	for _, side := range []struct {
		title string
		list  []comparison
	}{{"Wins", wins}, {"Losses", losses}, {"Ties", ties}} {
		b.WriteString("## " + side.title + "\n\n" + source + "\n\n")
		table(&b, []string{"Language", "Debug arm", "Print arm", "Debug", "Print", "Verdict"}, cells(side.list, func(c comparison) []string {
			return []string{c.tool.language, c.tool.arm, c.lines.arm, said(c.tool), said(c.lines), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## Every arm\n\n" + source + "\n\n")
	table(&b, []string{"Language", "Arm", "Call 3 shown", "Calls", "Tokens", "Wall", "Written", "Left", "Files off", "Failed"}, cells(rows, func(o outcome) []string {
		return []string{o.language, o.arm, fmt.Sprint(o.found), fmt.Sprint(o.calls), fmt.Sprint(tokens(o.bytes)), o.spent.Round(time.Millisecond).String(),
			fmt.Sprint(o.written), fmt.Sprint(o.left), listed(o.off), cell(o.failed)}
	}))
	return b.String()
}

func rank(o outcome) int {
	switch {
	case o.found && o.left == 0 && len(o.off) == 0:
		return 2
	case o.found:
		return 1
	}
	return 0
}

func judged(rows []outcome) []comparison {
	var out []comparison
	for _, tool := range rows {
		if tool.arm != armTrace && tool.arm != armBreak {
			continue
		}
		for _, lines := range rows {
			if (lines.arm == armPrintTool || lines.arm == armPrintShell) && lines.language == tool.language {
				out = append(out, comparison{tool: tool, lines: lines, verdict: verdict(rank(tool), rank(lines), tool.calls, lines.calls)})
			}
		}
	}
	return out
}

func verdict(toolRank, printRank, toolCalls, printCalls int) string {
	switch {
	case toolRank == 0 && printRank == 0:
		return fmt.Sprintf("tie: neither showed call 3, %d calls against %d", toolCalls, printCalls)
	case toolRank > printRank:
		return fmt.Sprintf("win: a better answer, %d calls against %d", toolCalls, printCalls)
	case toolRank < printRank:
		return fmt.Sprintf("loss: a worse answer, %d calls against %d", toolCalls, printCalls)
	case toolCalls < printCalls:
		return fmt.Sprintf("win: the same answer in %d calls against %d", toolCalls, printCalls)
	case toolCalls > printCalls:
		return fmt.Sprintf("loss: the same answer in %d calls against %d", toolCalls, printCalls)
	}
	return fmt.Sprintf("tie: the same answer in %d calls each", toolCalls)
}

func said(o outcome) string {
	shown := "call 3 shown"
	if !o.found {
		shown = "call 3 not shown"
	}
	return fmt.Sprintf("%s, %d calls, %d lines written, %d left", shown, o.calls, o.written, o.left)
}

func tldr(rows []outcome) string {
	var parts []string
	for _, arm := range arms {
		var runs, found, calls, bytes, written, left, failed int
		for _, o := range rows {
			if o.arm != arm {
				continue
			}
			runs, calls, bytes, written, left = runs+1, calls+o.calls, bytes+o.bytes, written+o.written, left+o.left
			if o.found {
				found++
			}
			if o.failed != "" {
				failed++
			}
		}
		parts = append(parts, fmt.Sprintf("%s: call 3 shown in %d of %d, %d calls, %d tokens, %d print lines written, %d left, failed in %d", arm, found, runs, calls, tokens(bytes), written, left, failed))
	}
	return strings.Join(parts, "; ") + "."
}

func tokens(bytes int) int {
	return (bytes + konst.SearchBytesPerToken - 1) / konst.SearchBytesPerToken
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
		if after, ok := strings.CutPrefix(line, "Version:"); ok {
			return strings.TrimSpace(after)
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

func listed(list []string) string {
	if len(list) == 0 {
		return "0"
	}
	return fmt.Sprintf("%d: %s", len(list), strings.Join(list, ", "))
}

func cell(s string) string {
	if s == "" {
		return "n/a"
	}
	return strings.ReplaceAll(s, "|", "/")
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
