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
	task, arm, call     string
	baseline, change    int
	baseCalls, newCalls int
	verdict             string
}

const cassetteDriver = "tofu run, cassette"

func tokens(bytes int) int {
	return (bytes + konst.SearchBytesPerToken - 1) / konst.SearchBytesPerToken
}

func render(rows []row, began time.Time, devCommit, scratch string) string {
	host, _ := os.Hostname()
	_, rtkErr := exec.LookPath("rtk")
	day := began.Format(time.DateOnly)
	var b strings.Builder
	fmt.Fprintf(&b, "# bench tools/current: the TOOLS1 tools against 0.5.21, %s\n\n", day)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Change build: %s. 0.5.21 build: %s. rtk on PATH: %t. ", host, began.Format("2006-01-02 15:04"), devCommit, oldCommit, rtkErr == nil)
	b.WriteString("Every arm: a fresh copy of the fixture and a fresh HOME under " + scratch + "/<dir>, every dir the same length because a result can print the project's absolute path, no classifier bound so no shell result is cut, no model call, no network, no warm-up. ")
	fmt.Fprintf(&b, "Tokens: result bytes over %d rounded up (konst.SearchBytesPerToken), the bytes being each tool result as the session recorded it, which is what the model is sent. ", konst.SearchBytesPerToken)
	b.WriteString("Calls: the scripted calls, and the first call whose result holds the answer; a cleanup call such as stopping the server runs and is not counted.\n\n")
	tasks, calls := judged(rows)
	taskWins, taskLosses := split(tasks)
	callWins, callLosses := split(calls)
	b.WriteString("## tldr\n\n" + tldr(rows, taskWins, taskLosses, callLosses) + "\n\n")
	taskSource := "Source: tofu run on a cassette, both builds, whole task, " + day + "."
	callSource := "Source: tofu run on a cassette, both builds, one call against the same step of 0.5.21, " + day + "."
	for _, side := range []struct {
		title        string
		tasks, calls []comparison
	}{{"Wins", taskWins, callWins}, {"Losses", taskLosses, callLosses}} {
		b.WriteString("## " + side.title + "\n\n" + taskSource + "\n\n")
		table(&b, []string{"Task", "Change arm", "0.5.21 tokens", "Change tokens", "Saved", "0.5.21 answer at call", "Change answer at call", "Verdict"}, cells(side.tasks, func(c comparison) []string {
			return []string{c.task, c.arm, fmt.Sprint(c.baseline), fmt.Sprint(c.change), fmt.Sprint(c.baseline - c.change), answerCell(c.baseCalls), answerCell(c.newCalls), c.verdict}
		}))
		b.WriteString("\n" + callSource + "\n\n")
		table(&b, []string{"Task", "Change arm", "Call", "0.5.21 tokens", "Change tokens", "Verdict"}, cells(side.calls, func(c comparison) []string {
			return []string{c.task, c.arm, c.call, fmt.Sprint(c.baseline), fmt.Sprint(c.change), c.verdict}
		}))
		b.WriteString("\n")
	}
	b.WriteString("## One test run, the first call of each fix task\n\nSource: the first tool result of each row, " + day + ".\n\n")
	var runs [][]string
	for _, r := range rows {
		if strings.HasPrefix(r.task, fixPrefix) && r.failed == "" {
			runs = append(runs, []string{r.task, r.arm, r.driver, fmt.Sprint(r.sizes[0]), fmt.Sprint(tokens(r.sizes[0]))})
		}
	}
	table(&b, []string{"Task", "Arm", "Driver", "Bytes", "Tokens"}, runs)
	b.WriteString("\n## Every row\n\nSource: this run, " + day + ".\n\n")
	table(&b, []string{"Task", "Arm", "Build", "Driver", "Dir", "Calls", "Answer at call", "Bytes", "Tokens", "Tokens per call", "Failed to run"}, cells(rows, func(r row) []string {
		perCall := make([]string, len(r.sizes))
		for i, size := range r.sizes {
			perCall[i] = fmt.Sprintf("%s %d", r.tools[i], tokens(size))
		}
		return []string{r.task, r.arm, r.build, r.driver, r.id, fmt.Sprint(len(r.sizes)), answerCell(r.answerAt), fmt.Sprint(r.bytes()), fmt.Sprint(tokens(r.bytes())),
			cellOr(strings.Join(perCall, ", "), "none"), cellOr(r.failed, "no")}
	}))
	b.WriteString("\n## Rerun\n\nFrom the repository root, with the two builds made as below:\n\n```\n")
	b.WriteString("git archive " + oldCommit + " --prefix=b0521/ | tar -x -C <scratch>\n")
	b.WriteString("(cd <scratch>/b0521 && GOMAXPROCS=4 go build -o <old exe> ./apps/cli)\n")
	b.WriteString("GOMAXPROCS=4 go build -o <dev exe> ./apps/cli\n")
	b.WriteString("go run ./bench/suites/tools/current " + strings.Join(os.Args[1:], " ") + "\n```\n")
	return b.String()
}

func judged(rows []row) (tasks, calls []comparison) {
	baselines := map[string]row{}
	for _, r := range rows {
		if r.build == oldBuild && r.driver == cassetteDriver {
			baselines[r.task] = r
		}
	}
	for _, r := range rows {
		base, found := baselines[r.task]
		if r.build != devBuild || r.driver != cassetteDriver || !found {
			continue
		}
		whole := comparison{task: r.task, arm: r.arm, baseline: tokens(base.bytes()), change: tokens(r.bytes()), baseCalls: base.answerAt, newCalls: r.answerAt}
		switch {
		case base.failed != "" || r.failed != "":
			whole.verdict = "an arm failed to run"
		case r.answerAt == 0:
			whole.verdict = "the change did not reach the answer"
		case base.answerAt == 0:
			whole.verdict = "win: 0.5.21 did not reach the answer"
		default:
			whole.verdict = verdict(whole.baseline, whole.change)
		}
		tasks = append(tasks, whole)
		for at := range min(len(base.sizes), len(r.sizes)) {
			one := comparison{task: r.task, arm: r.arm, call: fmt.Sprintf("%d: %s against %s", at+1, r.tools[at], base.tools[at]), baseline: tokens(base.sizes[at]), change: tokens(r.sizes[at])}
			one.verdict = verdict(one.baseline, one.change)
			calls = append(calls, one)
		}
	}
	return tasks, calls
}

func verdict(baseline, change int) string {
	switch {
	case change < baseline:
		return fmt.Sprintf("win: %.1fx fewer tokens", float64(baseline)/float64(max(change, 1)))
	case change == baseline:
		return "no change"
	}
	return fmt.Sprintf("loss: %.1fx more tokens", float64(change)/float64(max(baseline, 1)))
}

func split(list []comparison) (wins, losses []comparison) {
	for _, c := range list {
		switch {
		case strings.HasPrefix(c.verdict, "win"):
			wins = append(wins, c)
		case c.verdict != "no change" || c.call == "":
			losses = append(losses, c)
		}
	}
	return wins, losses
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

func tldr(rows []row, wins, losses, callLosses []comparison) string {
	saved, baseline := 0, 0
	for _, c := range wins {
		saved += c.baseline - c.change
		baseline += c.baseline
	}
	failed := 0
	for _, r := range rows {
		if r.failed != "" {
			failed++
		}
	}
	return fmt.Sprintf("%d change arms return fewer tokens than 0.5.21 over the whole task and %d do not; over the wins that is %d tokens fewer of %d (%.0f%%). %d single calls return more than the same step on 0.5.21. %d of %d rows failed to run and are rows below.",
		len(wins), len(losses), saved, baseline, 100*float64(saved)/float64(max(baseline, 1)), len(callLosses), failed, len(rows))
}

func answerCell(at int) string {
	if at == 0 {
		return "not reached"
	}
	return fmt.Sprint(at)
}

func cellOr(text, empty string) string {
	if text == "" {
		return empty
	}
	return strings.ReplaceAll(text, "|", "/")
}
