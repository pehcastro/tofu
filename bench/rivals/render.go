package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func renderMarkdown(rep report) string {
	var b strings.Builder
	b.WriteString("# " + rep.title() + "\n\n")
	for _, p := range rep.matchups {
		b.WriteString("- " + p.headline() + "\n")
	}
	b.WriteString("\n" + rep.scope() + "\n")
	for _, o := range []outcome{won, lost, tied} {
		b.WriteString(verdictTable(rep, o))
	}
	b.WriteString(sideCostTable(rep) + changeTable(rep) + mediansTable(rep) + loadMarkdown(rep) + setAsideTable(rep) + conditionsTable(rep))
	b.WriteString("\n" + rep.method)
	return b.String()
}

func (rep report) title() string {
	var rivals []string
	for _, a := range rep.present {
		if a.Tool != "tofu" {
			rivals = append(rivals, toolName(a.Tool))
		}
	}
	tofu := slices.IndexFunc(rep.present, func(a arm) bool { return a.Tool == "tofu" })
	last := len(rivals) - 1
	return fmt.Sprintf("bench rivals: tofu %s against %s and %s", rep.now[rep.present[tofu].Name][0].Version, strings.Join(rivals[:last], ", "), rivals[last])
}

func (rep report) scope() string {
	return fmt.Sprintf("%d live runs, %s, from `runs/%s`, against the runs of %s directly under `runs/`. A measure is a win or a loss only when the two spreads do not meet; otherwise it is a tie.",
		rep.runs(), rep.days(), rep.batch, rep.since)
}

func (p matchup) headline() string {
	wins, losses, ties := p.count(won), p.count(lost), p.count(tied)
	who := "level"
	switch {
	case wins > losses:
		who = "tofu ahead"
	case losses > wins:
		who = toolName(p.rival.Tool) + " ahead"
	}
	wall := p.wall()
	line := fmt.Sprintf("**%s**: %s, %s, %s, %s. Wall time is a %s, tofu %s at the median (%s s against %s s)", p.title(), who, counted(wins, "win", "wins"), counted(losses, "loss", "losses"), counted(ties, "tie", "ties"), wall.result, wall.howMuch(),
		wall.m.figure(wall.tofu.mid), wall.m.figure(wall.rival.mid))
	var best []string
	for _, d := range p.biggestWins(2) {
		best = append(best, d.m.name+" "+d.howMuch())
	}
	if len(best) > 0 {
		line += "; biggest wins: " + strings.Join(best, ", ")
	}
	return line + "."
}

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func verdictTable(rep report, o outcome) string {
	heads := map[outcome]string{won: "Where tofu wins", lost: "Where tofu loses", tied: "Where it is a tie"}
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n\n| pairing | measure | tofu | rival | tofu is | difference | on %s |\n|---|---|---|---|---|---|---|\n", heads[o], rep.since)
	for _, p := range rep.matchups {
		for _, d := range p.duels {
			if d.result == o {
				fmt.Fprintf(&b, "| %s, %s | %s | %s | %s | %s | %s | %s |\n", toolName(p.rival.Tool), p.model, d.m.label(), d.m.spread(d.tofu), d.m.spread(d.rival), d.howMuch(), d.difference(), d.before)
			}
		}
	}
	return b.String()
}

func sideCostTable(rep report) string {
	var b strings.Builder
	b.WriteString("\n## What tofu spends that the others do not\n\ntofu's memory model runs beside the conversation and no rival has one, so it is not a row above; its tokens are inside tofu's token totals.\n\n| arm | memory model calls per run | memory model |\n|---|---|---|\n")
	calls := measure{"memory model calls", "", "fewer", "more", 0, false, always(func(r row) float64 { return float64(r.Auxiliary) })}
	for _, a := range rep.present {
		rows := rep.now[a.Name]
		if a.Tool != "tofu" {
			continue
		}
		s, _ := statOf(rows, calls)
		side := distinct(rows, func(r row) string {
			return strings.Join(slices.DeleteFunc(slices.Clone(r.Models), func(m string) bool { return m == r.Model }), ", ")
		})
		fmt.Fprintf(&b, "| %s | %s | %s |\n", a.Name, calls.spread(s), side)
	}
	return b.String()
}

func changeTable(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Change since %s, medians\n\n| measure | %s |\n|---|%s\n", rep.since, strings.Join(armNames(rep.present), " | "), strings.Repeat("---|", len(rep.present)))
	for _, m := range measures {
		cells := []string{m.label()}
		for _, a := range rep.present {
			cells = append(cells, change(m, rep.then[a.Name], rep.now[a.Name]))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return b.String()
}

func change(m measure, then, now []row) string {
	after, okAfter := statOf(now, m)
	before, okBefore := statOf(then, m)
	switch {
	case !okAfter:
		return "not recorded"
	case !okBefore:
		return "not recorded → " + m.figure(after.mid)
	case before.mid == 0:
		return m.figure(before.mid) + " → " + m.figure(after.mid)
	}
	return fmt.Sprintf("%s → %s, %+.0f%%", m.figure(before.mid), m.figure(after.mid), (after.mid-before.mid)/before.mid*100)
}

func armNames(present []arm) []string {
	var names []string
	for _, a := range present {
		names = append(names, a.Name)
	}
	return names
}

func mediansTable(rep report) string {
	var b strings.Builder
	heads := []string{"arm", "runs"}
	for _, m := range measures {
		heads = append(heads, m.label())
	}
	fmt.Fprintf(&b, "\n## Medians per arm, lowest to highest in brackets\n\n| %s |\n|%s\n", strings.Join(heads, " | "), strings.Repeat("---|", len(heads)))
	for _, a := range rep.present {
		cells := []string{a.Name, fmt.Sprint(len(rep.now[a.Name]))}
		for _, m := range measures {
			cell := "not recorded"
			if s, ok := statOf(rep.now[a.Name], m); ok {
				cell = m.spread(s)
			}
			cells = append(cells, cell)
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return b.String()
}

func setAsideTable(rep report) string {
	var b strings.Builder
	b.WriteString("\n## Runs that fell short or were set aside\n\n| run file | arm | date | exit | quality | notes |\n|---|---|---|---|---|---|\n")
	short := func(file string, r row) {
		got, outOf := r.Score.quality()
		fmt.Fprintf(&b, "| `%s` | %s | %s | %d | %d of %d | %s |\n", file, r.Arm, r.Date, r.ExitCode, got, outOf, r.notes())
	}
	for _, rows := range []map[string][]row{rep.now, rep.then} {
		for _, a := range rep.present {
			for _, r := range rows[a.Name] {
				if got, outOf := r.Score.quality(); got < outOf || r.ExitCode != 0 {
					short(r.file(), r)
				}
			}
		}
	}
	for _, r := range rep.setAside {
		short("runs/"+rep.batch+"/discarded/"+r.name()+".json", r)
	}
	return b.String()
}

func conditionsTable(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Conditions\n\n| arm | build on %s | build now | models the responses reported | thinking sent | sub-agents | credential | machine | dates |\n|---|---|---|---|---|---|---|---|---|\n", rep.since)
	for _, a := range rep.present {
		rows := rep.now[a.Name]
		first, last := rows[0], rows[len(rows)-1]
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s to %s |\n", a.Name, distinct(rep.then[a.Name], row.build), distinct(rows, row.build),
			distinct(rows, func(r row) string { return strings.Join(r.Models, ", ") }),
			distinct(rows, func(r row) string { return strings.ReplaceAll(cmp.Or(r.ThinkingSent, "not read"), "|", "/") }),
			distinct(rows, func(r row) string { return fmt.Sprint(r.Agents) }),
			distinct(rows, func(r row) string { return r.Credential }), first.Machine, first.Date, last.Date)
	}
	b.WriteString("\n| arm | posture |\n|---|---|\n")
	for _, a := range rep.present {
		fmt.Fprintf(&b, "| %s | %s |\n", a.Name, distinct(rep.now[a.Name], func(r row) string { return r.Posture }))
	}
	return b.String()
}

func appendixMarkdown(rep report) (string, error) {
	var b strings.Builder
	b.WriteString("\n## Appendix\n\nEvery table below is printed from the run files as they are.\n")
	fmt.Fprintf(&b, "\nProcess tree: %s.\n", treeMethod)
	runsTable(&b, rep.now)
	wallTable(&b, rep.now)
	toolTable(&b, rep.now)
	err := taskTables(&b)
	return b.String(), err
}

func runsTable(b *strings.Builder, byArm map[string][]row) {
	b.WriteString("\n### Every run\n\n| arm | rep | exit | wall s | CPU s | peak tree RSS MiB | peak commit MiB | processes | conversation requests | fresh in | cache read | cache write | out | calls | failed | retried | hidden of 5 | rubric of 5 | lines +/- | notes |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(byArm) {
		for _, r := range byArm[a.Name] {
			fmt.Fprintf(b, "| %s | %d | %d | %.1f | %.1f | %.0f | %.0f | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | +%d/-%d | %s |\n",
				r.Arm, r.Rep, r.ExitCode, seconds(r.WallMS), seconds(r.Tree.CPUMS), megabytes(r.Tree.PeakTreeRSS), megabytes(r.Tree.PeakCommit), r.Tree.Processes,
				r.Requests, r.Tokens.FreshIn, r.Tokens.CacheRead, r.Tokens.CacheWrite, r.Tokens.Out, len(r.Calls), failedCalls(r), r.Retried,
				r.Score.hiddenPassed(), r.Score.rubricPassed(), r.Score.Added, r.Score.Deleted, r.notes())
		}
	}
}

type wallSplit struct {
	r                                                  row
	model, firstToken, afterFirst, tools, own, outside float64
}

func wallSplits(rows []row) []wallSplit {
	var out []wallSplit
	for _, r := range rows {
		if len(r.Timed) == 0 {
			continue
		}
		var model, first int64
		for _, timed := range r.Timed {
			model, first = model+timed.MS, first+timed.FirstMS
		}
		last := r.Timed[len(r.Timed)-1]
		span := seconds(last.AtMS + last.MS)
		tools := toolSeconds(r)
		out = append(out, wallSplit{r, seconds(model), seconds(first), seconds(model - first), tools, span - seconds(model) - tools, seconds(r.WallMS) - span})
	}
	return out
}

func wallTable(b *strings.Builder, byArm map[string][]row) {
	b.WriteString("\n### Where the wall time goes\n\nOne row per run whose transcript times each model request: tofu from `tofu session request`, codex from its rollout file, where the first token is the first finished output item and so an upper bound. Own work is the time between requests that no tool call covers, and outside is the wall clock before the first request and after the last.\n\n| arm | rep | wall s | requests | model wait s | to first token s | after first token s | tools s | own work s | outside s |\n|---|---|---|---|---|---|---|---|---|---|\n")
	var pooled []string
	for _, a := range present(byArm) {
		var times []requestTime
		for _, w := range wallSplits(byArm[a.Name]) {
			times = append(times, w.r.Timed...)
			fmt.Fprintf(b, "| %s | %d | %.1f | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f |\n", w.r.Arm, w.r.Rep, seconds(w.r.WallMS), len(w.r.Timed), w.model, w.firstToken, w.afterFirst, w.tools, w.own, w.outside)
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
	b.WriteString("\n### Tool against tool\n\nEvery call of the runs pooled; calls per run is the median. tofu reports each call's duration itself, the other three are timed here from the call event on stdout to its result.\n\n| arm | kind | calls per run | p50 ms | p95 ms | max ms | failed | untimed |\n|---|---|---|---|---|---|---|---|\n")
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

func taskTables(b *strings.Builder) error {
	names, err := taskNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		rows, err := readRows(filepath.Join(here(), "runs", name, "*.json"))
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			taskTable(b, name, rows)
		}
	}
	return nil
}

func taskTable(b *strings.Builder, name string, rows []row) {
	byArm := byArm(rows)
	_, outOf := rows[0].Score.quality()
	fmt.Fprintf(b, "\n### Task %s, from `runs/%s`\n\nPrompt, rubric and seed in `testdata/tasks/%s`. Memory or judge calls are the calls outside the conversation, tofu's memory model and omp's auto-thinking judge.\n\n", name, name, name)
	b.WriteString("| arm | tool version | runs | quality of " + fmt.Sprint(outOf) + " | conversation requests | memory or judge calls | fresh in | cache read | cache write | total in | out | wall s | peak tree RSS MiB | failed calls | sub-agents |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range present(byArm) {
		armRows := byArm[a.Name]
		column := func(value func(row) float64) string { return spread(armRows, value) }
		fmt.Fprintf(b, "| %s | %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", a.Name, distinct(armRows, row.build), len(armRows),
			column(func(r row) float64 { got, _ := r.Score.quality(); return float64(got) }),
			column(func(r row) float64 { return float64(r.Requests) }),
			column(func(r row) float64 { return float64(r.Auxiliary) }),
			column(func(r row) float64 { return float64(r.Tokens.FreshIn) }),
			column(func(r row) float64 { return float64(r.Tokens.CacheRead) }),
			column(func(r row) float64 { return float64(r.Tokens.CacheWrite) }),
			column(func(r row) float64 { return float64(r.Tokens.in()) }),
			column(func(r row) float64 { return float64(r.Tokens.Out) }),
			column(func(r row) float64 { return seconds(r.WallMS) }),
			column(func(r row) float64 { return megabytes(r.Tree.PeakTreeRSS) }),
			column(func(r row) float64 { return float64(failedCalls(r)) }),
			column(func(r row) float64 { return float64(r.Agents) }))
	}
}
