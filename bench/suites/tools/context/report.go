package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
)

type metric struct {
	name   string
	lower  bool
	of     func(outcome) int
	spread bool
}

func metrics() []metric {
	return []metric{
		{name: "peak window tokens, one request", lower: true, of: func(o outcome) int { return o.peak }, spread: true},
		{name: "window tokens, last request", lower: true, of: func(o outcome) int { return o.last }, spread: true},
		{name: "window tokens summed over requests", lower: true, of: func(o outcome) int { return o.sum }, spread: true},
		{name: "requests", lower: true, of: func(o outcome) int { return o.requests }},
		{name: "reads of build.log", lower: true, of: func(o outcome) int { return o.logReads }},
		{name: "calls fetching a trimmed or dropped result back, artifact_fetch and history", lower: true, of: func(o outcome) int { return o.calls["artifact_fetch"] + o.calls["history"] }},
		{name: "runs where go test . passes", of: passes},
	}
}

func passes(o outcome) int {
	if o.passed {
		return 1
	}
	return 0
}

func render(rows []outcome, began time.Time, m machine, built string) string {
	host, _ := os.Hostname()
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/context: window size over a long session, tools on and off, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s, %s/%s, %d logical CPUs. Date: %s. Both arms built back to back from one tree, %s; the off build adds -overlay dropping the five constructors from leadToolConstructors in apps/cli/run.go. go: %s.\n\n", host, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), began.Format("2006-01-02 15:04"), built, version("go"))
	fmt.Fprintf(&b, "Credential: claude-sub subscription, a VACUUM INTO copy of the owner's login in a fresh HOME per run, the classifier role file copied for the gate. Wire: anthropic, effort medium, --context-ceiling %d on every run, so the notice line is %.0f tokens. Model asked: %s; model the response reported is per row. No API key: CLAUDE*, ANTHROPIC*, OPENROUTER* and *_API_KEY are dropped from every run's environment. Network: required.\n\n", m.ceiling, float64(m.ceiling)*konst.ContextForgetOfferShare, m.model)
	fmt.Fprintf(&b, "Every run: one session, no warm-up and nothing discarded, runs one at a time and alternating on, off, on, off. A fresh copy of bench/suites/tools/fixtures/go under %s/<arm>-<run> with a build.log of about %d bytes written into it, its exact size per row: download noise, then the real go test -v output of that copy's failing TestDiscounted, then cleanup noise, as TOFU-2389 used. Window tokens of a request: in plus cache read plus cache write as the response reported it, which counts the system prompt and the tool definitions the request carried. Passed: go test -count=1 . on the copy after the run.\n\n", m.scratch, logBytes)
	b.WriteString("Arms: on is the dev build as it stands; off is the same build, the same task and the same ceiling with context, budget, forget, note and digest not offered, which also turns off the notice since it fires only when forget is offered. The off arm is what the five replace: the log stays in the window until tofu's own trim and compaction take it.\n\n")
	b.WriteString("Task, the same for both arms: " + task + "\n\n")
	b.WriteString("## tldr\n\n" + tldr(rows) + "\n\n")
	source := "Source: tofu run on the dev build and on the off build, turn against turn, " + day + "."
	for _, kind := range []struct{ title, verdict string }{{"Wins", "win"}, {"Losses", "loss"}, {"Ties", "tie"}} {
		fmt.Fprintf(&b, "## %s, tools on against tools off\n\n%s\n\n", kind.title, source)
		var cells [][]string
		for _, mt := range metrics() {
			mine, other := pick(rows, "on", mt), pick(rows, "off", mt)
			if verdict(mt, mine, other) == kind.verdict {
				cells = append(cells, []string{mt.name, summary(mine, mt.spread), summary(other, mt.spread), kind.verdict})
			}
		}
		table(&b, []string{"Measure", "Tools on", "Tools off", "Verdict"}, cells)
		b.WriteString("\n")
	}
	b.WriteString("## Every run\n\nSource: every run of bench/suites/tools/context, " + day + ".\n\n")
	var cells [][]string
	for _, o := range rows {
		cells = append(cells, []string{o.arm.key, fmt.Sprint(o.run), fmt.Sprint(o.passed), fmt.Sprint(o.peak), fmt.Sprint(o.peakStep), fmt.Sprint(o.last), fmt.Sprint(o.sum), fmt.Sprint(o.requests), fmt.Sprint(o.out), fmt.Sprint(o.logSize), fmt.Sprint(o.logReads), fmt.Sprint(o.notices), fmt.Sprint(o.offered), own(o), o.spent.Round(time.Second).String(), cell(o.model), cell(o.check), cell(o.note), cell(o.failed)})
	}
	table(&b, []string{"Arm", "Run", "Passed", "Peak window", "Peak at request", "Last window", "Window summed", "Requests", "Tokens out", "build.log bytes", "build.log reads", "Notices", "forget offered", "Own and fetch tools called", "Wall", "Model reported", "Check after", "Note", "Failed"}, cells)
	return b.String()
}

func pick(rows []outcome, key string, mt metric) []int {
	var values []int
	for _, o := range rows {
		if o.arm.key == key && o.failed == "" {
			values = append(values, mt.of(o))
		}
	}
	slices.Sort(values)
	return values
}

func middle(values []int) int {
	return values[(len(values)-1)/2]
}

func summary(values []int, spread bool) string {
	if len(values) == 0 {
		return "no run finished"
	}
	if !spread {
		return fmt.Sprintf("%d over %d runs", sum(values), len(values))
	}
	return fmt.Sprintf("median %d, min %d, max %d, %d runs", middle(values), values[0], values[len(values)-1], len(values))
}

func sum(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}

func verdict(mt metric, mine, other []int) string {
	if len(mine) == 0 || len(other) == 0 {
		return "loss"
	}
	a, b := middle(mine), middle(other)
	if !mt.spread {
		a, b = sum(mine), sum(other)
	}
	switch {
	case a == b:
		return "tie"
	case (a < b) == mt.lower:
		return "win"
	}
	return "loss"
}

func tldr(rows []outcome) string {
	var lines []string
	for _, mt := range metrics() {
		on, off := pick(rows, "on", mt), pick(rows, "off", mt)
		if !mt.spread || len(on) == 0 || len(off) == 0 {
			continue
		}
		a, b := middle(on), middle(off)
		lines = append(lines, fmt.Sprintf("- %s, median: %d tools on, %d tools off, %+d (%+.1f%%), %s; on %d to %d, off %d to %d", mt.name, a, b, a-b, 100*float64(a-b)/float64(b), verdict(mt, on, off), on[0], on[len(on)-1], off[0], off[len(off)-1]))
	}
	return cmp.Or(strings.Join(lines, "\n"), fmt.Sprintf("nothing compared: %d on runs and %d off runs finished", len(pick(rows, "on", metrics()[0])), len(pick(rows, "off", metrics()[0]))))
}

func own(o outcome) string {
	var said []string
	for _, name := range slices.Concat(ownTools, []string{"artifact_fetch", "history"}) {
		if o.calls[name] > 0 {
			said = append(said, fmt.Sprintf("%s %d", name, o.calls[name]))
		}
	}
	return cmp.Or(strings.Join(said, ", "), "none")
}

func cell(s string) string {
	return cmp.Or(strings.NewReplacer("|", "/", "\n", " ").Replace(strings.TrimSpace(s)), "-")
}

func table(b *strings.Builder, header []string, rows [][]string) {
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|" + strings.Repeat(" --- |", len(header)) + "\n")
	if len(rows) == 0 {
		b.WriteString("| none |" + strings.Repeat(" |", len(header)-1) + "\n")
	}
	for _, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
}

func version(exe string) string {
	said, err := exec.Command(exe, "version").CombinedOutput()
	if err != nil {
		return "unknown: " + err.Error()
	}
	return strings.Join(strings.Fields(string(said)), " ")
}
