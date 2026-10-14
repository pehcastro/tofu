package main

import (
	"cmp"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const idleGap = 15 * time.Minute

const dateLayout = "2006-01-02"

type window struct {
	since, until time.Time
}

func (w window) holds(at time.Time) bool {
	return !at.IsZero() && (w.since.IsZero() || !at.Before(w.since)) && (w.until.IsZero() || at.Before(w.until))
}

type tally struct {
	sessions, generations, subTranscripts int
	requests, untimedRequests, calls      int
	unreadable                            int
	first, last                           time.Time
	byBucket                              map[bucket]time.Duration
	byTool                                map[string]time.Duration
}

func newTally() *tally {
	return &tally{byBucket: map[bucket]time.Duration{}, byTool: map[string]time.Duration{}}
}

func (t *tally) add(kind bucket, tool string, spent time.Duration, at time.Time) {
	t.byBucket[kind] += spent
	if tool != "" {
		t.byTool[tool] += spent
	}
	if t.first.IsZero() || at.Before(t.first) {
		t.first = at
	}
	if at.After(t.last) {
		t.last = at
	}
}

func main() {
	home, _ := os.UserHomeDir()
	project := flag.String("project", ".", "the project whose tofu sessions are read")
	claude := flag.String("claude", filepath.Join(home, ".claude", "projects"), "Claude Code's projects folder")
	since := flag.String("since", "", "first day counted, "+dateLayout)
	until := flag.String("until", "", "last day counted, "+dateLayout)
	tools := flag.Int("tools", 12, "rows in each by-tool table")
	flag.Parse()
	var w window
	var err error
	if *since != "" {
		w.since, err = time.ParseInLocation(dateLayout, *since, time.Local)
	}
	if err == nil && *until != "" {
		w.until, err = time.ParseInLocation(dateLayout, *until, time.Local)
		w.until = w.until.AddDate(0, 0, 1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sessiontime:", err)
		os.Exit(2)
	}
	fmt.Printf("where session time goes, run %s by tofu %s commit %s on %s/%s with %d cores, window %s to %s\n",
		time.Now().Format(dateLayout), konst.Version, sys.BuildRevision(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), cmp.Or(*since, "the start"), cmp.Or(*until, "today"))
	fmt.Println("times are agent-hours summed over every agent, not wall clock; reads local files only, no network")
	tofu, err := readTofu(*project, w)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sessiontime: tofu:", err)
		os.Exit(1)
	}
	fmt.Printf("\ntofu: %d sessions (%d generations, %d unreadable), %s to %s, %d requests (%d with no recorded duration), %d tool calls\n",
		tofu.sessions, tofu.generations, tofu.unreadable, day(tofu.first), day(tofu.last), tofu.requests, tofu.untimedRequests, tofu.calls)
	printTally(tofu, *tools)
	code, err := readClaude(*claude, w)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sessiontime: claude code:", err)
		os.Exit(1)
	}
	fmt.Printf("\nclaude code: %d main sessions plus %d sub-agent transcripts, %s to %s, %d requests, %d tool calls; each gap between two transcript lines is charged to the later line\n",
		code.sessions, code.subTranscripts, day(code.first), day(code.last), code.requests, code.calls)
	printTally(code, *tools)
}

func day(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return at.Local().Format(dateLayout)
}

func printTally(t *tally, tools int) {
	var total time.Duration
	for _, kind := range counted {
		total += t.byBucket[kind]
	}
	share := func(spent time.Duration) string {
		if total == 0 {
			return "-"
		}
		return fmt.Sprintf("%5.1f%%", 100*spent.Hours()/total.Hours())
	}
	row := func(name string, spent time.Duration, shown string) {
		fmt.Printf("  %-50s %9.2f  %s\n", name, spent.Hours(), shown)
	}
	fmt.Printf("  %-50s %9s  %s\n", "bucket", "hours", "share")
	rows := slices.Clone(counted)
	slices.SortStableFunc(rows, func(a, b bucket) int { return cmp.Compare(t.byBucket[b], t.byBucket[a]) })
	for _, kind := range rows {
		row(string(kind), t.byBucket[kind], share(t.byBucket[kind]))
	}
	row("total", total, share(total))
	fmt.Println("  left out of the total:")
	for _, kind := range excluded {
		row(string(kind), t.byBucket[kind], "")
	}
	names := slices.SortedFunc(maps.Keys(t.byTool), func(a, b string) int { return cmp.Or(cmp.Compare(t.byTool[b], t.byTool[a]), cmp.Compare(a, b)) })
	names = names[:min(tools, len(names))]
	fmt.Printf("  %-50s %9s  %s\n", "by tool, top "+fmt.Sprint(len(names)), "hours", "share")
	for _, name := range names {
		row(name, t.byTool[name], share(t.byTool[name]))
	}
}
