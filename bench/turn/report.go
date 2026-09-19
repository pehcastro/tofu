package turn

import (
	"fmt"
	"strings"
	"time"

	"boji/bench/report"
)

func Render(result Result, conditions report.Conditions) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench turn: %s\n\n", conditions.Date)
	fmt.Fprintf(b, "Machine: %s. Credential kind: %s. Wire: %s. Model: `%s`.\n\n", conditions.Machine, conditions.CredentialKind, conditions.Wire, result.Model)
	fmt.Fprintf(b, "Total spend of this run: $%.6f over %d turns, summed from `usage.cost` on each model response, never from a price table. Run this on purpose, not twice by accident.\n\n",
		result.TotalCostUSD, result.TotalTurns)
	fmt.Fprintf(b, "%d tasks, %d reps each, two arms (loop and capped at one step): %d turns total, %d failed.\n\n",
		len(Tasks()), Reps, result.TotalTurns, result.FailedRuns)
	b.WriteString("No first run was discarded as a warm-up; every run below, including the first, counts. Every task runs in a fresh, disposable scratch directory outside this repository, made with `os.MkdirTemp`, one per run, removed after the run.\n\n")

	renderTaskSections(b, result)
	renderFailures(b, result)

	return b.String()
}

func renderTaskSections(b *strings.Builder, result Result) {
	b.WriteString("## Per task, per arm\n\n")
	b.WriteString("The loop arm runs with a step cap of 5. The off arm, capped at one step, is the same task and the same prompt: it shows what a single model call achieves without a loop.\n\n")
	b.WriteString("| Task | Arm | Runs | Passed | Failed | Timed runs | Median wall clock | p95 wall clock | Min | Max | Median steps | Median cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range result.Stats {
		p95Note := ""
		if s.TimedRuns < 10 {
			p95Note = fmt.Sprintf(" (n=%d, not a real p95, treat as directional)", s.TimedRuns)
		}
		fmt.Fprintf(b, "| %s | %s | %d | %d | %d | %d | %.0f ms | %.0f ms%s | %.0f ms | %.0f ms | %.1f | $%.6f |\n",
			s.Task, s.Arm, s.Runs, s.Passed, s.Failed, s.TimedRuns,
			s.MedianWallClockMS, s.P95WallClockMS, p95Note, s.MinWallClockMS, s.MaxWallClockMS, s.MedianSteps, s.MedianCostUSD)
	}
	b.WriteString("\nA percentile above resting on fewer than ten timed runs is the second-largest value in a small sample, not a measured tail; the table marks every one of those.\n\n")

	b.WriteString("## Tool calls by kind, passed runs only\n\n")
	b.WriteString("| Task | Arm | Tool calls |\n|---|---|---|\n")
	for _, s := range result.Stats {
		fmt.Fprintf(b, "| %s | %s | %s |\n", s.Task, s.Arm, formatToolCalls(s.ToolCalls))
	}
	b.WriteString("\n")
}

func formatToolCalls(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	var parts []string
	for _, name := range []string{"read", "write", "bash"} {
		if n, ok := counts[name]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", name, n))
		}
	}
	return strings.Join(parts, ", ")
}

func renderFailures(b *strings.Builder, result Result) {
	b.WriteString("## Failed runs\n\n")
	var failed []RunResult
	for _, r := range result.Runs {
		if !r.Passed {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		b.WriteString("None. Every run's outcome check passed.\n\n")
		return
	}
	fmt.Fprintf(b, "%d of %d runs failed their outcome check and are excluded from the timing figures above.\n\n", len(failed), result.TotalTurns)
	b.WriteString("| Task | Arm | Rep | Outcome | Reason |\n|---|---|---|---|---|\n")
	for _, r := range failed {
		reason := r.CheckNote
		if r.CallErr != "" {
			reason = r.CallErr
		}
		fmt.Fprintf(b, "| %s | %s | %d | %s | %s |\n", r.Task, r.Arm, r.Rep, r.Row.Outcome, reason)
	}
	b.WriteString("\n")
}

func Filename(now time.Time) string {
	return fmt.Sprintf("report-%s.md", now.Format("2006-01-02"))
}
