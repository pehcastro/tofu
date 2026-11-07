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
	tool, edit outcome
	verdict    string
}

func render(rows, untouched []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/change: codemod against edit, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Dev build: %s. ", host, began.Format("2006-01-02 15:04"), version(m.exe, "version"))
	for _, tool := range [][]string{{"go", "version"}, {"node", "--version"}, {"tsc", "--version"}, {"python", "--version"}, {"cargo", "--version"}} {
		fmt.Fprintf(&b, "%s: %s. ", tool[0], version(tool[0], tool[1:]...))
	}
	b.WriteString("Credential: none, no model call, no network (GOPROXY=off, cargo --offline). Every arm: one run, no warm-up, a fresh copy of the fixture and a fresh HOME under " + m.scratch + "/<dir>. ")
	b.WriteString("Off arm: tofu tool edit with replace_all on the literal, the text change an agent makes without codemod. ")
	fmt.Fprintf(&b, "Tokens: stdout bytes of every call over %d rounded up. Calls: tofu tool invocations. Wrong: a line changed that should not be, or changed to other text than the expected line. Tests: the language's test command, compared with the untouched copy.\n\n", konst.SearchBytesPerToken)
	baseline := map[string]string{}
	for _, o := range untouched {
		baseline[o.language] = o.tests
	}
	var wins, losses, ties []comparison
	for _, c := range judged(rows, baseline) {
		switch {
		case strings.HasPrefix(c.verdict, "win"):
			wins = append(wins, c)
		case strings.HasPrefix(c.verdict, "loss"):
			losses = append(losses, c)
		default:
			ties = append(ties, c)
		}
	}
	b.WriteString("## tldr\n\n" + tldr(rows, baseline, len(wins), len(losses), len(ties)) + "\n\n")
	source := "Source: tofu tool codemod and tofu tool edit on the dev build, bench/suites/tools/change, " + day + "."
	for _, side := range []struct {
		title string
		list  []comparison
	}{{"Wins", wins}, {"Losses", losses}, {"Ties", ties}} {
		b.WriteString("## " + side.title + "\n\n" + source + "\n\n")
		table(&b, []string{"Language", "Task", "Codemod missed", "Codemod wrong", "Codemod tests", "Edit missed", "Edit wrong", "Edit tests", "Verdict"}, cells(side.list, func(c comparison) []string {
			return []string{c.tool.language, c.tool.task, fmt.Sprint(len(c.tool.missed)), fmt.Sprint(len(c.tool.wrong)), cell(c.tool.tests), fmt.Sprint(len(c.edit.missed)), fmt.Sprint(len(c.edit.wrong)), cell(c.edit.tests), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## Every arm\n\n" + source + "\n\n")
	table(&b, []string{"Language", "Task", "Arm", "True sites", "Missed", "Wrong", "Tests after", "Calls", "Tokens", "Wall", "Failed"}, cells(rows, func(o outcome) []string {
		return []string{o.language, o.task, o.arm, fmt.Sprint(o.want), sites(o.missed), sites(o.wrong), cell(o.tests), fmt.Sprint(o.calls), fmt.Sprint((o.bytes + konst.SearchBytesPerToken - 1) / konst.SearchBytesPerToken), o.spent.Round(time.Millisecond).String(), cell(o.failed)}
	}))
	b.WriteString("\n## Tests on an untouched copy\n\nSource: the test command of each language on a fresh fixture copy, " + day + ".\n\n")
	table(&b, []string{"Language", "Tests", "Failed"}, cells(untouched, func(o outcome) []string { return []string{o.language, cell(o.tests), cell(o.failed)} }))
	return b.String()
}

func judged(rows []outcome, baseline map[string]string) []comparison {
	var out []comparison
	for _, tool := range rows {
		if tool.arm != armTool {
			continue
		}
		for _, edit := range rows {
			if edit.arm != armEdit || edit.language != tool.language || edit.task != tool.task {
				continue
			}
			c := comparison{tool: tool, edit: edit}
			toolOff, editOff := len(tool.missed)+len(tool.wrong), len(edit.missed)+len(edit.wrong)
			toolBreaks, editBreaks := tool.tests != baseline[tool.language], edit.tests != baseline[edit.language]
			switch {
			case tool.failed != "" && edit.failed == "":
				c.verdict = "loss: the codemod arm failed"
			case toolOff < editOff:
				c.verdict = fmt.Sprintf("win: %d sites off against %d", toolOff, editOff)
			case toolOff > editOff:
				c.verdict = fmt.Sprintf("loss: %d sites off against %d", toolOff, editOff)
			case editBreaks && !toolBreaks:
				c.verdict = "win: same sites, edit changes the tests"
			case toolBreaks && !editBreaks:
				c.verdict = "loss: same sites, codemod changes the tests"
			default:
				c.verdict = fmt.Sprintf("tie: %d sites off each, %d calls against %d", toolOff, tool.calls, edit.calls)
			}
			out = append(out, c)
		}
	}
	return out
}

func tldr(rows []outcome, baseline map[string]string, wins, losses, ties int) string {
	type total struct{ off, broke, failed, runs, calls, bytes int }
	sum := map[string]*total{}
	var order []string
	for _, o := range rows {
		if sum[o.arm] == nil {
			sum[o.arm] = &total{}
			order = append(order, o.arm)
		}
		t := sum[o.arm]
		t.runs++
		t.off += len(o.missed) + len(o.wrong)
		t.calls += o.calls
		t.bytes += o.bytes
		if o.tests != baseline[o.language] {
			t.broke++
		}
		if o.failed != "" {
			t.failed++
		}
	}
	var parts []string
	for _, arm := range order {
		t := sum[arm]
		parts = append(parts, fmt.Sprintf("%s: %d sites wrong or missed, tests changed in %d of %d runs, failed to run in %d, %d calls, %d tokens", arm, t.off, t.broke, t.runs, t.failed, t.calls, (t.bytes+konst.SearchBytesPerToken-1)/konst.SearchBytesPerToken))
	}
	return strings.Join(parts, "; ") + fmt.Sprintf(". Codemod wins %d comparisons, loses %d and ties %d.", wins, losses, ties)
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
