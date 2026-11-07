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
	job       string
	tool, git outcome
	verdict   string
}

const (
	jobName    = "name the culprit"
	jobRestore = "undo the culprit, keep the rest"
)

func render(rows []outcome, began time.Time, m machine) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/history: bisect and trail against git by hand, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Dev build: %s. ", host, began.Format("2006-01-02 15:04"), version(m.exe, "version"))
	for _, tool := range [][]string{{"git", "--version"}, {"go", "version"}, {"python", "--version"}} {
		fmt.Fprintf(&b, "%s: %s. ", tool[0], version(tool[0], tool[1:]...))
	}
	b.WriteString("Credential: none, no model call, no network (GOPROXY=off); the eight turns are tofu drive on a cassette, one drive per turn, one session. Every arm: one run, no warm-up, a fresh copy of the fixture and a fresh HOME under " + m.scratch + "/<dir>, git init and one commit before the session, then a hand edit to .env.example by the person after the last turn. ")
	b.WriteString("Off arm: git by hand, the thing bisect and trail replace, because git is what an agent reaches for today to find and undo a bad change; it runs twice, with the session's edits uncommitted as tofu leaves them, and with a commit after every turn, which is git's best case. ")
	fmt.Fprintf(&b, "Calls: tofu tool or shell commands, each counted once. Tokens: output bytes of every call over %d rounded up. Named: the turn and the file the arm points at; the uncommitted arm can name a file only. Moved: files the naming left changed. Restore: exact means every file equals the fixture with every turn but the culprit and the person's edit; refused means nothing was written.\n\n", konst.SearchBytesPerToken)
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
	b.WriteString("## tldr\n\n" + tldr(rows, len(wins), len(losses), len(ties)) + "\n\n")
	source := "Source: tofu tool bisect and tofu tool trail on the dev build against git by a fixed procedure, bench/suites/tools/history, " + day + "."
	for _, side := range []struct {
		title string
		list  []comparison
	}{{"Wins", wins}, {"Losses", losses}, {"Ties", ties}} {
		b.WriteString("## " + side.title + "\n\n" + source + "\n\n")
		table(&b, []string{"Language", "Scenario", "Job", "Git arm", "Tool", "Git", "Verdict"}, cells(side.list, func(c comparison) []string {
			return []string{c.tool.language, c.tool.scenario, c.job, c.git.arm, said(c.job, c.tool), said(c.job, c.git), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## Every arm\n\n" + source + "\n\n")
	table(&b, []string{"Language", "Scenario", "Arm", "Culprit", "Named turn", "Named file", "Name calls", "Name tokens", "Name wall", "Moved", "Restore", "Restore calls", "Restore tokens", "Restore wall", "Files off", "Person kept", "Refused", "Failed"}, cells(rows, func(o outcome) []string {
		return []string{o.language, o.scenario, o.arm, fmt.Sprintf("turn %d of %d, %s", o.culprit, o.culpritTurns, o.culpritFile), turnCell(o.namedTurn), cell(o.namedFile),
			fmt.Sprint(o.nameCalls), fmt.Sprint(tokens(o.nameBytes)), o.nameSpent.Round(time.Millisecond).String(), listed(o.treeMoved),
			o.restored.String(), fmt.Sprint(o.restoreCalls), fmt.Sprint(tokens(o.restoreBytes)), o.restoreSpent.Round(time.Millisecond).String(), listed(o.off), fmt.Sprint(o.personKept), cell(o.refused), cell(o.failed)}
	}))
	return b.String()
}

func nameRank(o outcome) int {
	switch {
	case o.namedTurn == o.culprit && o.namedFile == o.culpritFile && len(o.treeMoved) == 0:
		return 3
	case o.namedFile == o.culpritFile && len(o.treeMoved) == 0:
		return 2
	case o.namedFile == o.culpritFile:
		return 1
	}
	return 0
}

func judged(rows []outcome) []comparison {
	var out []comparison
	for _, tool := range rows {
		if tool.arm != armTool {
			continue
		}
		for _, git := range rows {
			if git.arm == armTool || git.language != tool.language || git.scenario != tool.scenario {
				continue
			}
			out = append(out,
				comparison{job: jobName, tool: tool, git: git, verdict: verdict(nameRank(tool), nameRank(git), tool.nameCalls, git.nameCalls)},
				comparison{job: jobRestore, tool: tool, git: git, verdict: verdict(int(tool.restored), int(git.restored), tool.restoreCalls, git.restoreCalls)})
		}
	}
	return out
}

func verdict(toolRank, gitRank, toolCalls, gitCalls int) string {
	switch {
	case toolRank > gitRank:
		return fmt.Sprintf("win: a better answer, %d calls against %d", toolCalls, gitCalls)
	case toolRank < gitRank:
		return fmt.Sprintf("loss: a worse answer, %d calls against %d", toolCalls, gitCalls)
	case toolCalls < gitCalls:
		return fmt.Sprintf("win: the same answer in %d calls against %d", toolCalls, gitCalls)
	case toolCalls > gitCalls:
		return fmt.Sprintf("loss: the same answer in %d calls against %d", toolCalls, gitCalls)
	}
	return fmt.Sprintf("tie: the same answer in %d calls each", toolCalls)
}

func said(job string, o outcome) string {
	if job == jobRestore {
		return fmt.Sprintf("%s, %d calls, files off %s", o.restored, o.restoreCalls, listed(o.off))
	}
	return fmt.Sprintf("%s, %s, %d calls, moved %s", turnCell(o.namedTurn), cell(o.namedFile), o.nameCalls, listed(o.treeMoved))
}

func tldr(rows []outcome, wins, losses, ties int) string {
	type total struct{ runs, turn, file, nameCalls, nameBytes, exact, refused, wrong, restoreCalls, restoreBytes, failed int }
	sum := map[string]*total{}
	var order []string
	for _, o := range rows {
		if sum[o.arm] == nil {
			sum[o.arm] = &total{}
			order = append(order, o.arm)
		}
		t := sum[o.arm]
		t.runs++
		t.nameCalls, t.nameBytes, t.restoreCalls, t.restoreBytes = t.nameCalls+o.nameCalls, t.nameBytes+o.nameBytes, t.restoreCalls+o.restoreCalls, t.restoreBytes+o.restoreBytes
		if o.namedTurn == o.culprit {
			t.turn++
		}
		if o.namedFile == o.culpritFile {
			t.file++
		}
		switch o.restored {
		case exact:
			t.exact++
		case refusedClean:
			t.refused++
		case wrong:
			t.wrong++
		case notTried:
		}
		if o.failed != "" {
			t.failed++
		}
	}
	var parts []string
	for _, arm := range order {
		t := sum[arm]
		parts = append(parts, fmt.Sprintf("%s: culprit turn named in %d of %d, file in %d, %d calls and %d tokens to name; restore exact in %d, refused in %d, wrong in %d, %d calls and %d tokens; failed in %d",
			arm, t.turn, t.runs, t.file, t.nameCalls, tokens(t.nameBytes), t.exact, t.refused, t.wrong, t.restoreCalls, tokens(t.restoreBytes), t.failed))
	}
	return strings.Join(parts, "; ") + fmt.Sprintf(". The tools win %d comparisons, lose %d and tie %d.", wins, losses, ties)
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
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

func turnCell(n int) string {
	if n <= 0 {
		return "none"
	}
	return fmt.Sprintf("turn %d", n)
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
