package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var kinds = []string{"read", "edit", "write", "search", "bash", "spawn", "other"}

func writeReport([]string) error {
	paths, err := filepath.Glob(filepath.Join(here(), "runs", "*.json"))
	if err != nil {
		return err
	}
	byArm := map[string][]row{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r row
		if err := json.Unmarshal(body, &r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		byArm[r.Arm] = append(byArm[r.Arm], r)
	}
	judgement, err := os.ReadFile(filepath.Join(here(), "judgement.md"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var b strings.Builder
	b.Write(judgement)
	conditions(&b, byArm)
	runsTable(&b, byArm)
	turnTable(&b, byArm)
	toolTable(&b, byArm)
	return os.WriteFile(filepath.Join(here(), "report.md"), []byte(b.String()), 0o644)
}

func present(byArm map[string][]row) []arm {
	return slices.DeleteFunc(slices.Clone(arms), func(a arm) bool { return len(byArm[a.Name]) == 0 })
}

func conditions(b *strings.Builder, byArm map[string][]row) {
	fmt.Fprintf(b, "\n## Conditions\n\nProcess tree: %s.\n\n", treeMethod)
	b.WriteString("| arm | tool version | asked for | every model the responses reported | credential | posture | machine | dates |\n|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(byArm) {
		rows := byArm[a.Name]
		first, last := rows[0], rows[len(rows)-1]
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s to %s |\n", a.Name, distinct(rows, row.build), first.AskedModel, distinct(rows, func(r row) string { return strings.Join(r.Models, ", ") }),
			distinct(rows, func(r row) string { return r.Credential }), first.Posture, first.Machine, first.Date, last.Date)
	}
}

func (r row) build() string {
	if r.Tool == "tofu" {
		return fmt.Sprintf("%s commit %s", r.Version, cmp.Or(fmt.Sprintf("%.8s", r.Commit), "not recorded"))
	}
	return r.Version
}

func distinct(rows []row, field func(row) string) string {
	var seen []string
	for _, r := range rows {
		if v := field(r); !slices.Contains(seen, v) {
			seen = append(seen, v)
		}
	}
	return strings.Join(seen, "; ")
}

func runsTable(b *strings.Builder, byArm map[string][]row) {
	b.WriteString("\n## Every run\n\n| arm | rep | exit | wall s | CPU s | peak tree RSS MB | peak commit MB | processes | requests | fresh in | cache read | cache write | out | calls | failed | retried | hidden of 5 | rubric of 5 | lines +/- | notes |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(byArm) {
		for _, r := range byArm[a.Name] {
			notes := append(slices.Clone(r.Notes), r.Score.Failures...)
			if len(r.Score.Stray) > 0 {
				notes = append(notes, "stray: "+strings.Join(r.Score.Stray, ", "))
			}
			if len(r.Score.HarnessFiles) > 0 {
				notes = append(notes, "harness files left in the repo: "+strings.Join(r.Score.HarnessFiles, ", "))
			}
			fmt.Fprintf(b, "| %s | %d | %d | %.1f | %.1f | %.0f | %.0f | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | +%d/-%d | %s |\n",
				r.Arm, r.Rep, r.ExitCode, seconds(r.WallMS), seconds(r.Tree.CPUMS), megabytes(r.Tree.PeakTreeRSS), megabytes(r.Tree.PeakCommit), r.Tree.Processes,
				r.Requests, r.Tokens.FreshIn, r.Tokens.CacheRead, r.Tokens.CacheWrite, r.Tokens.Out, len(r.Calls), failedCalls(r), r.Retried,
				r.Score.hiddenPassed(), r.Score.rubricPassed(), r.Score.Added, r.Score.Deleted, strings.ReplaceAll(strings.Join(notes, "; "), "|", "/"))
		}
	}
}

func turnTable(b *strings.Builder, byArm map[string][]row) {
	b.WriteString("\n## Turn against turn\n\nMedian of the runs, with the lowest and highest in brackets. Total in is fresh plus cache read plus cache write.\n\n| arm | runs | quality of 10 | wall s | CPU s | peak tree RSS MB | requests | total in | fresh in | out | calls | failed | retried | lines changed |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(byArm) {
		rows := byArm[a.Name]
		column := func(value func(row) float64) string { return spread(rows, value) }
		fmt.Fprintf(b, "| %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", a.Name, len(rows),
			column(func(r row) float64 { return float64(r.Score.hiddenPassed() + r.Score.rubricPassed()) }),
			column(func(r row) float64 { return seconds(r.WallMS) }),
			column(func(r row) float64 { return seconds(r.Tree.CPUMS) }),
			column(func(r row) float64 { return megabytes(r.Tree.PeakTreeRSS) }),
			column(func(r row) float64 { return float64(r.Requests) }),
			column(func(r row) float64 { return float64(r.Tokens.in()) }),
			column(func(r row) float64 { return float64(r.Tokens.FreshIn) }),
			column(func(r row) float64 { return float64(r.Tokens.Out) }),
			column(func(r row) float64 { return float64(len(r.Calls)) }),
			column(func(r row) float64 { return float64(failedCalls(r)) }),
			column(func(r row) float64 { return float64(r.Retried) }),
			column(func(r row) float64 { return float64(r.Score.Added + r.Score.Deleted) }))
	}
}

func toolTable(b *strings.Builder, byArm map[string][]row) {
	b.WriteString("\n## Tool against tool\n\nEvery call of the three runs pooled. Calls per run is the median. Time is from the tool's own transcript: tofu reports each call's duration itself, the other three are timed here from the moment the call event reached stdout to the moment its result did. An untimed call is one whose start the transcript did not show.\n\n| arm | kind | calls per run | p50 ms | p95 ms | max ms | failed | untimed |\n|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(byArm) {
		rows := byArm[a.Name]
		for _, kind := range kinds {
			var perRun []float64
			var times []int64
			failed, untimed := 0, 0
			for _, r := range rows {
				n := 0
				for _, c := range r.Calls {
					if c.Kind != kind {
						continue
					}
					n++
					if c.Failed {
						failed++
					}
					if c.MS < 0 {
						untimed++
						continue
					}
					times = append(times, c.MS)
				}
				perRun = append(perRun, float64(n))
			}
			if slices.Max(perRun) == 0 {
				continue
			}
			slices.Sort(times)
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %d | %d |\n", a.Name, kind, trim(median(perRun)), rank(times, 0.5), rank(times, 0.95), rank(times, 1), failed, untimed)
		}
	}
}

func failedCalls(r row) int {
	n := 0
	for _, c := range r.Calls {
		if c.Failed {
			n++
		}
	}
	return n
}

func spread(rows []row, value func(row) float64) string {
	values := make([]float64, 0, len(rows))
	for _, r := range rows {
		values = append(values, value(r))
	}
	return fmt.Sprintf("%s [%s to %s]", trim(median(values)), trim(slices.Min(values)), trim(slices.Max(values)))
}

func median(values []float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func rank(sorted []int64, quantile float64) string {
	if len(sorted) == 0 {
		return "none"
	}
	index := max(int(math.Ceil(quantile*float64(len(sorted))))-1, 0)
	return fmt.Sprint(sorted[index])
}

func trim(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func seconds(ms int64) float64 {
	return float64(ms) / 1000
}

func megabytes(bytes uint64) float64 {
	return float64(bytes) / (1 << 20)
}
