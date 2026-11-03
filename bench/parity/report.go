package main

import (
	"fmt"
	"runtime"
	"strings"
)

const (
	diffContext  = 3
	diffLines    = 40
	snippetReach = 60
)

func renderReport(sides []build, cat catalog, results []result) string {
	var r strings.Builder
	counts := map[outcome]int{}
	for _, one := range results {
		counts[one.outcome]++
	}
	r.WriteString("# tofu parity\n\n| side | exe | sha256 |\n|---|---|---|\n")
	for _, side := range sides {
		fmt.Fprintf(&r, "| %s | %s | %s |\n", side.label, side.exe, side.sum)
	}
	fmt.Fprintf(&r, "\nMachine %s, %s/%s. Each case runs once per build, a then b, one process at a time, in a home, an APPDATA and a project made fresh for it, with no key and every network URL pointed at a refused port. No call is discarded as a warm up. Masked: paths, versions, the commit field, Go version, dates, clock times, ago and just now in a padded column, padding before a session name, milliseconds, KB sizes, pids, uuids, session ids and names, tool refs; a byte count may differ by %d; a transcript that holds a session name, whose length is random, is compared as words, so its line breaks may move; serve notifications are sorted, without seq and session, turn.steered without its turn, and the session.listed broadcast is dropped because how often it fires follows timing; files written are compared by path, without sqlite -shm and -wal files and shells/pruned, which follow timing too.\n\n",
		hostname(), runtime.GOOS, runtime.GOARCH, byteSlack)
	fmt.Fprintf(&r, "**%d/%d equal**, %d diff, %d error, %d manual, %d unreachable\n\n", counts[outcomeEqual], len(results), counts[outcomeDiff], counts[outcomeError], len(cat.manual), len(cat.unreachable))
	r.WriteString("## Diffs\n\n")
	if counts[outcomeEqual] == len(results) {
		r.WriteString("None.\n\n")
	}
	for _, one := range results {
		if one.outcome != outcomeEqual {
			fmt.Fprintf(&r, "### %s: %s (%s)\n\n```\n%s\n```\n\n", one.outcome, one.c.name, one.c.from, strings.TrimRight(one.detail, "\n"))
		}
	}
	r.WriteString("## Cases\n\n| case | from | steps | result |\n|---|---|---|---|\n")
	for _, one := range results {
		fmt.Fprintf(&r, "| %s | %s | %d | %s |\n", one.c.name, one.c.from, len(one.c.steps), one.outcome)
	}
	r.WriteString("\n## Manual\n\nNot offline. Each is driven by hand, with these steps, on both builds.\n\n")
	for i, m := range cat.manual {
		fmt.Fprintf(&r, "### %d. %s\n\n%s\n\n", i+1, m.name, m.why)
		for j, s := range m.steps {
			fmt.Fprintf(&r, "%d. %s\n", j+1, s)
		}
		r.WriteString("\n")
	}
	r.WriteString("## Unreachable\n\n| item | why |\n|---|---|\n")
	for _, u := range cat.unreachable {
		fmt.Fprintf(&r, "| %s | %s |\n", u.name, u.why)
	}
	return r.String()
}

func window(a, b string) string {
	left, right := strings.Split(a, "\n"), strings.Split(b, "\n")
	head := 0
	for head < len(left) && head < len(right) && left[head] == right[head] {
		head++
	}
	tail := 0
	for tail < len(left)-head && tail < len(right)-head && left[len(left)-1-tail] == right[len(right)-1-tail] {
		tail++
	}
	var out strings.Builder
	fmt.Fprintf(&out, "@@ line %d\n", head+1)
	if head < len(left)-tail && head < len(right)-tail {
		a, b := left[head], right[head]
		at := 0
		for at < len(a) && at < len(b) && a[at] == b[at] {
			at++
		}
		from := max(0, at-snippetReach)
		fmt.Fprintf(&out, "@@ column %d: a ...%s... b ...%s...\n", at+1, a[from:min(len(a), at+snippetReach)], b[from:min(len(b), at+snippetReach)])
	}
	for _, line := range left[max(0, head-diffContext):head] {
		out.WriteString("  " + line + "\n")
	}
	for _, side := range []struct {
		lines []string
		mark  string
	}{{left[head : len(left)-tail], "- "}, {right[head : len(right)-tail], "+ "}} {
		for i, line := range side.lines {
			if i == diffLines {
				fmt.Fprintf(&out, "%s... %d more lines\n", side.mark, len(side.lines)-diffLines)
				break
			}
			out.WriteString(side.mark + line + "\n")
		}
	}
	return out.String()
}
