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
	byArm, bySetting := map[string][]row{}, map[string][]row{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r row
		if err := json.Unmarshal(body, &r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if r.Setting != "" {
			bySetting[r.Arm] = append(bySetting[r.Arm], r)
			continue
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
	wallTable(&b, byArm)
	toolTable(&b, byArm)
	settingTable(&b, bySetting)
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

func wallTable(b *strings.Builder, byArm map[string][]row) {
	b.WriteString("\n## Where the wall time goes\n\nOne row per run whose transcript times each model request: tofu from `tofu session request`, codex from its rollout file. Model wait is every request from sent to its last byte, split at the first token; for codex the first token is the first finished output item, so its share is an upper bound. Own work is the time between requests that no tool call covers; it goes below zero when a tool starts before its request finishes streaming. Outside is the wall clock before the first request and after the last.\n\n| arm | rep | wall s | requests | model wait s | to first token s | after first token s | tools s | own work s | outside s |\n|---|---|---|---|---|---|---|---|---|---|\n")
	var pooled []string
	for _, a := range present(byArm) {
		var times []requestTime
		for _, r := range byArm[a.Name] {
			if len(r.Timed) == 0 {
				continue
			}
			times = append(times, r.Timed...)
			var model, first, tools int64
			for _, timed := range r.Timed {
				model, first = model+timed.MS, first+timed.FirstMS
			}
			for _, c := range r.Calls {
				tools += max(c.MS, 0)
			}
			last := r.Timed[len(r.Timed)-1]
			span := last.AtMS + last.MS
			fmt.Fprintf(b, "| %s | %d | %.1f | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f |\n", r.Arm, r.Rep, seconds(r.WallMS), len(r.Timed), seconds(model), seconds(first),
				seconds(model-first), seconds(tools), seconds(span-model-tools), seconds(r.WallMS-span))
		}
		if len(times) > 1 {
			fixed, perToken := fitRequests(times)
			pooled = append(pooled, fmt.Sprintf("| %s | %d | %.1f | %.1f |", a.Name, len(times), fixed/1000, perToken))
		}
	}
	b.WriteString("\nEvery timed request of an arm pooled, fitted as a fixed wait plus a time per output token, least squares.\n\n| arm | requests | fixed s per request | ms per output token |\n|---|---|---|---|\n")
	b.WriteString(strings.Join(pooled, "\n") + "\n")
}

func fitRequests(times []requestTime) (float64, float64) {
	var sumOut, sumMS float64
	for _, timed := range times {
		sumOut, sumMS = sumOut+float64(timed.Out), sumMS+float64(timed.MS)
	}
	meanOut, meanMS := sumOut/float64(len(times)), sumMS/float64(len(times))
	var covariance, variance float64
	for _, timed := range times {
		covariance += (float64(timed.Out) - meanOut) * (float64(timed.MS) - meanMS)
		variance += (float64(timed.Out) - meanOut) * (float64(timed.Out) - meanOut)
	}
	slope := covariance / variance
	return meanMS - slope*meanOut, slope
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

const (
	settingShipped = "shipped"
	settingRival   = "rival"
)

var settingMeasures = []func(row) float64{
	func(r row) float64 { return seconds(r.WallMS) },
	func(r row) float64 { return float64(r.Requests) },
	func(r row) float64 { return float64(r.Tokens.in()) },
	func(r row) float64 { return float64(r.Tokens.Out) },
	func(r row) float64 { return float64(r.Tokens.Reasoning) },
	func(r row) float64 { return float64(r.Score.hiddenPassed() + r.Score.rubricPassed()) },
}

func settingTable(b *strings.Builder, bySetting map[string][]row) {
	if len(bySetting) == 0 {
		return
	}
	b.WriteString("\n## Shipped thinking setting against the rival's\n\nOne row per run, alternating. Shipped is tofu as built; rival is the same commit sending what the rival on that wire sends. Thinking sent is read from the first request body tofu recorded. Total in is fresh plus cache read plus cache write; reasoning is the wire's own count and is part of out.\n\n| arm | setting | rep | date | thinking sent | wall s | requests | total in | out | reasoning | quality of 10 |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(bySetting) {
		for _, r := range slices.SortedFunc(slices.Values(bySetting[a.Name]), func(x, y row) int { return cmp.Compare(x.Date, y.Date) }) {
			fmt.Fprintf(b, "| %s | %s | %d | %s | `%s` | %.1f | %d | %d | %d | %d | %d |\n", r.Arm, r.Setting, r.Rep, r.Date, r.ThinkingSent, seconds(r.WallMS), r.Requests,
				r.Tokens.in(), r.Tokens.Out, r.Tokens.Reasoning, r.Score.hiddenPassed()+r.Score.rubricPassed())
		}
	}
	b.WriteString("\nMedians per setting, and what shipped costs over rival in percent of the rival's median.\n\n| arm | setting | runs | wall s | requests | total in | out | reasoning | quality of 10 |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(bySetting) {
		shipped, rival := settingMedians(b, a.Name, settingShipped, bySetting[a.Name]), settingMedians(b, a.Name, settingRival, bySetting[a.Name])
		if len(shipped) == 0 || len(rival) == 0 {
			continue
		}
		cells := []string{a.Name, "shipped over rival", ""}
		for i := range settingMeasures {
			cells = append(cells, percentOver(shipped[i], rival[i]))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
}

func settingMedians(b *strings.Builder, armName, setting string, rows []row) []float64 {
	chosen := slices.DeleteFunc(slices.Clone(rows), func(r row) bool { return r.Setting != setting })
	if len(chosen) == 0 {
		return nil
	}
	cells := []string{armName, setting, fmt.Sprint(len(chosen))}
	var medians []float64
	for _, measure := range settingMeasures {
		cells = append(cells, spread(chosen, measure))
		medians = append(medians, median(valuesOf(chosen, measure)))
	}
	b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	return medians
}

func percentOver(shipped, rival float64) string {
	if rival == 0 {
		return "no rival value"
	}
	return fmt.Sprintf("%+.1f%%", (shipped-rival)/rival*100)
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

func valuesOf(rows []row, value func(row) float64) []float64 {
	values := make([]float64, 0, len(rows))
	for _, r := range rows {
		values = append(values, value(r))
	}
	return values
}

func spread(rows []row, value func(row) float64) string {
	values := valuesOf(rows, value)
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
