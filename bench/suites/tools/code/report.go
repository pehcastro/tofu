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
	language, task, grep string
	tool, other          outcome
	verdict              string
}

func render(rows, untouched []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/code: symbols rename and impact against grep, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Dev build: %s. ", host, began.Format("2006-01-02 15:04"), version(m.exe, "version"))
	for _, tool := range [][]string{{"go", "version"}, {"node", "--version"}, {"tsc", "--version"}, {"typescript-language-server", "--version"}, {"rust-analyzer", "--version"}, {"pyright", "--version"}, {"python", "--version"}} {
		fmt.Fprintf(&b, "%s: %s. ", tool[0], version(tool[0], tool[1:]...))
	}
	b.WriteString("Credential: none, no model call, no network (GOPROXY=off, cargo --offline). Every arm: one run, no warm-up, a fresh copy of the fixture and a fresh HOME under " + m.scratch + "/<dir>. ")
	fmt.Fprintf(&b, "Tokens: output bytes over %d rounded up, the tool's stdout or the grep listing path:line:text. Calls: 1 for the tool, 2 for a grep rename (list, then replace in place), 1 for a grep caller search. ", konst.SearchBytesPerToken)
	b.WriteString("Build: go vet, tsc on the non-test sources, cargo test --no-run, and a python import that calls Order.discounted, which reaches the renamed function.\n\n")
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
	b.WriteString("## tldr\n\n" + tldr(rows, len(wins), len(losses), len(ties)) + "\n\n")
	source := "Source: tofu tool symbols on the dev build against the Go regexp arms in bench/suites/tools/code, " + day + "."
	for _, side := range []struct {
		title string
		list  []comparison
	}{{"Wins", wins}, {"Losses", losses}, {"Ties", ties}} {
		b.WriteString("## " + side.title + "\n\n" + source + "\n\n")
		table(&b, []string{"Language", "Task", "Grep arm", "Tool missed", "Tool wrong", "Tool build", "Grep missed", "Grep wrong", "Grep build", "Verdict"}, cells(side.list, func(c comparison) []string {
			return []string{c.language, c.task, c.grep, fmt.Sprint(len(c.tool.missed)), fmt.Sprint(len(c.tool.wrong)), cell(c.tool.build), fmt.Sprint(len(c.other.missed)), fmt.Sprint(len(c.other.wrong)), cell(c.other.build), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## Every arm\n\n" + source + "\n\n")
	table(&b, []string{"Language", "Task", "Arm", "True sites", "Missed", "Wrongly changed or found", "Build after", "Calls", "Tokens", "Wall", "Failed"}, cells(rows, func(o outcome) []string {
		return []string{o.language, o.task, o.arm, fmt.Sprint(len(o.want)), sites(o.missed), sites(o.wrong), cell(o.build), fmt.Sprint(o.calls), fmt.Sprint((o.bytes + konst.SearchBytesPerToken - 1) / konst.SearchBytesPerToken), o.spent.Round(time.Millisecond).String(), cell(o.failed)}
	}))
	b.WriteString("\n## The build on an untouched copy\n\nSource: the build command of each language on a fresh fixture copy, " + day + ".\n\n")
	table(&b, []string{"Language", "Build", "Failed"}, cells(untouched, func(o outcome) []string { return []string{o.language, o.build, cell(o.failed)} }))
	return b.String()
}

func judged(rows []outcome) []comparison {
	var out []comparison
	for _, tool := range rows {
		if tool.arm != armTool {
			continue
		}
		for _, other := range rows {
			if other.arm == armTool || other.language != tool.language || other.task != tool.task {
				continue
			}
			c := comparison{language: tool.language, task: tool.task, grep: other.arm, tool: tool, other: other}
			toolOff, otherOff := len(tool.missed)+len(tool.wrong), len(other.missed)+len(other.wrong)
			toolBreaks, otherBreaks := strings.HasPrefix(tool.build, "fails"), strings.HasPrefix(other.build, "fails")
			switch {
			case tool.failed != "":
				c.verdict = "loss: the tool arm failed"
			case other.failed != "":
				c.verdict = "tie: the grep arm failed"
			case toolOff < otherOff:
				c.verdict = fmt.Sprintf("win: %d sites off against %d", toolOff, otherOff)
			case toolOff > otherOff:
				c.verdict = fmt.Sprintf("loss: %d sites off against %d", toolOff, otherOff)
			case otherBreaks && !toolBreaks:
				c.verdict = "win: same sites, grep breaks the build"
			case toolBreaks && !otherBreaks:
				c.verdict = "loss: same sites, the tool breaks the build"
			default:
				c.verdict = fmt.Sprintf("tie: %d sites off each", toolOff)
			}
			out = append(out, c)
		}
	}
	return out
}

func tldr(rows []outcome, wins, losses, ties int) string {
	type total struct{ missed, wrong, broke, failed, runs int }
	sum := map[string]*total{}
	var order []string
	for _, o := range rows {
		if o.task != taskRename {
			continue
		}
		if sum[o.arm] == nil {
			sum[o.arm] = &total{}
			order = append(order, o.arm)
		}
		t := sum[o.arm]
		t.runs++
		t.missed += len(o.missed)
		t.wrong += len(o.wrong)
		if strings.HasPrefix(o.build, "fails") {
			t.broke++
		}
		if o.failed != "" {
			t.failed++
		}
	}
	var parts []string
	for _, arm := range order {
		t := sum[arm]
		parts = append(parts, fmt.Sprintf("%s missed %d sites, wrongly changed %d, broke the build in %d of %d languages and failed to run in %d", arm, t.missed, t.wrong, t.broke, t.runs, t.failed))
	}
	return "On the alias rename: " + strings.Join(parts, "; ") + fmt.Sprintf(". Over rename and find callers the tool wins %d comparisons, loses %d and ties %d.", wins, losses, ties)
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

func sites(list []string) string {
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
