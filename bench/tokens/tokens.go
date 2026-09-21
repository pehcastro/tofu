package tokens

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/konst"
	"tofu/internal/turn/tools"
)

type ToolTotals struct {
	Tool        string
	Calls       int
	BeforeBytes int64
	AfterBytes  int64
}

type GlobCall struct {
	Turn        string
	Step        int
	Args        string
	BeforeBytes int64
	AfterBytes  int64
	Matched     int
	Returned    int
	ReplayError string
}

type Result struct {
	SessionsDir string
	Turns       int
	Skipped     []corpus.SkippedTurn
	Totals      []ToolTotals
	GlobCalls   []GlobCall
}

func Run(sessionsDir, repoRoot string) (Result, error) {
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	glob, err := tools.NewGlob(repoRoot)
	if err != nil {
		return Result{}, err
	}
	byTool := map[string]*ToolTotals{}
	result := Result{SessionsDir: sessionsDir, Turns: len(walked.Turns), Skipped: walked.Skipped}
	for _, turn := range walked.Turns {
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				totals, ok := byTool[call.Tool]
				if !ok {
					totals = &ToolTotals{Tool: call.Tool}
					byTool[call.Tool] = totals
				}
				totals.Calls++
				totals.BeforeBytes += call.ResultBytes
				if call.Tool != "glob" || len(call.Args) == 0 {
					totals.AfterBytes += call.ResultBytes
					continue
				}
				replayed := replayGlob(glob, call)
				replayed.Turn = turn.ID
				replayed.Step = step.Index
				result.GlobCalls = append(result.GlobCalls, replayed)
				totals.AfterBytes += replayed.AfterBytes
			}
		}
	}
	for _, totals := range byTool {
		result.Totals = append(result.Totals, *totals)
	}
	sort.Slice(result.Totals, func(i, j int) bool { return result.Totals[i].BeforeBytes > result.Totals[j].BeforeBytes })
	return result, nil
}

func replayGlob(glob tools.Glob, call corpus.RecordedCall) GlobCall {
	replayed := GlobCall{
		Args:        strings.Join(strings.Fields(string(call.Args)), " "),
		BeforeBytes: call.ResultBytes,
		AfterBytes:  call.ResultBytes,
	}
	capped, err := glob.Run(context.Background(), call.Args)
	if err != nil {
		replayed.ReplayError = err.Error()
		return replayed
	}
	if simulated := int64(len(capped.Content)); simulated < call.ResultBytes {
		replayed.AfterBytes = simulated
	}
	if _, err := fmt.Sscanf(capped.Content, "%d of", &replayed.Matched); err != nil {
		replayed.Matched = 0
	}
	replayed.Returned = min(replayed.Matched, konst.GlobPathsResultCap)
	return replayed
}

func Render(result Result) string {
	b := &strings.Builder{}
	var before, after int64
	for _, t := range result.Totals {
		before += t.BeforeBytes
		after += t.AfterBytes
	}

	fmt.Fprintf(b, "sessions read: %d turns under %s, %d skipped\n", result.Turns, result.SessionsDir, len(result.Skipped))
	for _, s := range result.Skipped {
		fmt.Fprintf(b, "  skipped: %s: %s\n", s.Path, s.Reason)
	}
	b.WriteString("\n")

	renderTable(b, "before the glob cap", result.Totals, before, func(t ToolTotals) int64 { return t.BeforeBytes })
	b.WriteString("\n")
	renderTable(b, "after the glob cap", result.Totals, after, func(t ToolTotals) int64 { return t.AfterBytes })
	b.WriteString("\n")

	if before > 0 {
		saved := float64(before-after) / float64(before) * 100
		fmt.Fprintf(b, "total bytes: %d before, %d after, %.1f%% saved\n", before, after, saved)
	}
	b.WriteString("\n")
	renderGlobCalls(b, result.GlobCalls)
	b.WriteString("dollars: $0.00 in every row. every model in this table ran on a subscription, which spends a quota window rather than money, and catalog/models carries no per-token rate for a subscription model. this is a named skip: the dollar column cannot be computed from the catalog as it stands.\n")
	return b.String()
}

func renderGlobCalls(b *strings.Builder, calls []GlobCall) {
	fmt.Fprintf(b, "every recorded glob call, %d of them, replayed against the tree as it stands today\n", len(calls))
	fmt.Fprintf(b, "%-26s %5s %10s %10s %10s %10s  %s\n", "turn", "step", "matched", "returned", "was bytes", "now bytes", "args")
	for _, c := range calls {
		if c.ReplayError != "" {
			fmt.Fprintf(b, "%-26s %5d %10s %10s %10d %10d  %s: replay failed: %s\n", c.Turn, c.Step, "skipped", "skipped", c.BeforeBytes, c.AfterBytes, c.Args, c.ReplayError)
			continue
		}
		fmt.Fprintf(b, "%-26s %5d %10d %10d %10d %10d  %s\n", c.Turn, c.Step, c.Matched, c.Returned, c.BeforeBytes, c.AfterBytes, c.Args)
	}
}

func renderTable(b *strings.Builder, title string, totals []ToolTotals, total int64, pick func(ToolTotals) int64) {
	fmt.Fprintf(b, "%s\n", title)
	fmt.Fprintf(b, "%-16s %8s %14s %12s %10s %8s\n", "tool", "calls", "bytes", "avg bytes", "tokens", "share")
	for _, t := range totals {
		bytes := pick(t)
		avg := int64(0)
		if t.Calls > 0 {
			avg = bytes / int64(t.Calls)
		}
		share := 0.0
		if total > 0 {
			share = float64(bytes) / float64(total) * 100
		}
		fmt.Fprintf(b, "%-16s %8d %14d %12d %10d %7.1f%%\n", t.Tool, t.Calls, bytes, avg, bytes/konst.SearchBytesPerToken, share)
	}
}
