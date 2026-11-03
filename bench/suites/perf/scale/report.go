package main

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
)

const (
	memoryBudgetMB     = 2048
	coreBudget         = 2
	extrapolationTimes = 10
	armConditions      = "Each arm is one run per rung, driven by `run.sh` in a scratch project outside any repository, the task being one file read per sub-agent and its first line repeated back. tofu: `tofu run --no-instructions --no-gate --sift free`, so no Jev call is made; on the cassette the replies are scripted, so answered there only says every sub-agent turn ran. Claude Code: `claude -p --setting-sources project --strict-mcp-config`, so none of the owner's hooks or MCP servers start. omp: `omp -p --no-extensions --no-skills --no-rules --no-session` with `omp-overlay.yml` binding the task role to Sonnet; its own MCP config still loads. codex: `codex exec --sandbox read-only` on the owner's config with the model forced to gpt-5.6-sol, because its default is Astra; it runs a different model from the other three, so its numbers compare harness against harness only loosely. Cores is CPU seconds over wall seconds; a live run waits on the network most of the time, so the 2 vCPU column says how many such runs side by side would fill two cores."
)

type line struct{ base, perAgent float64 }

type fits struct{ peak, steady, commit, cpu, load line }

func fit(rows []row, measure func(row) float64) line {
	var n, sx, sy, sxx, sxy float64
	for _, r := range rows {
		x, y := float64(r.Agents), measure(r)
		n, sx, sy, sxx, sxy = n+1, sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	slope := (n*sxy - sx*sy) / (n*sxx - sx*sx)
	return line{base: (sy - slope*sx) / n, perAgent: slope}
}

func cores(r row) float64 { return r.CPUS / r.WallS }

func fitGroup(rows []row) fits {
	return fits{
		peak:   fit(rows, func(r row) float64 { return r.PeakRSSMB }),
		steady: fit(rows, func(r row) float64 { return r.SteadyRSSMB }),
		commit: fit(rows, func(r row) float64 { return r.PeakCommitMB }),
		cpu:    fit(rows, func(r row) float64 { return r.CPUS }),
		load:   fit(rows, cores),
	}
}

func fitsIn(budget float64, l line, measuredTo int) string {
	if l.base >= budget {
		return "none, the base alone is over"
	}
	if l.perAgent <= 0 {
		return "no limit found, the fitted cost per agent is not positive"
	}
	agents := int(math.Floor((budget - l.base) / l.perAgent))
	if agents > extrapolationTimes*measuredTo {
		return fmt.Sprintf("over %d, a straight line past %d measured", extrapolationTimes*measuredTo, measuredTo)
	}
	return fmt.Sprint(agents)
}

func summary(text *strings.Builder, groups [][]row) {
	text.WriteString("## Summary\n\n")
	text.WriteString("| tool | arm | model | peak RSS base MB | peak RSS per agent MB | commit base MB | commit per agent MB | CPU base s | CPU per agent s | agents in 2 GB by peak RSS | agents in 2 vCPU |\n")
	text.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, rows := range groups {
		f, last := fitGroup(rows), rows[len(rows)-1]
		fmt.Fprintf(text, "| %s | %s | %s | %.0f | %.1f | %.0f | %.1f | %.2f | %.2f | %s | %s |\n",
			last.Tool, last.Arm, last.Model, f.peak.base, f.peak.perAgent, f.commit.base, f.commit.perAgent, f.cpu.base, f.cpu.perAgent,
			fitsIn(memoryBudgetMB, f.peak, last.Agents), fitsIn(coreBudget, f.load, last.Agents))
	}
	text.WriteString("\n")
}

func toolSection(text *strings.Builder, rows []row) {
	first := rows[0]
	fmt.Fprintf(text, "## %s, %s arm, %s credential\n\n", first.Tool, first.Arm, first.Credential)
	text.WriteString("| agents | answered | wall s | peak RSS MB | steady RSS MB | peak commit MB | CPU s | cores | processes | exit | model | wire | version | date |\n")
	text.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(text, "| %d | %d/%d | %.1f | %.0f | %.0f | %.0f | %.2f | %.2f | %d | %d | %s | %s | %s | %s |\n",
			r.Agents, r.Answered, max(r.Agents, 1), r.WallS, r.PeakRSSMB, r.SteadyRSSMB, r.PeakCommitMB, r.CPUS, cores(r), r.PeakProcesses, r.Exit, r.Model, r.Wire, cmp.Or(r.Version, "not recorded"), r.Date)
	}
	f, measuredTo := fitGroup(rows), rows[len(rows)-1].Agents
	fmt.Fprintf(text, "\n- peak RSS: base %.0f MB, %.1f MB per agent; agents that fit in 2 GB: %s\n", f.peak.base, f.peak.perAgent, fitsIn(memoryBudgetMB, f.peak, measuredTo))
	fmt.Fprintf(text, "- steady RSS: base %.0f MB, %.1f MB per agent\n", f.steady.base, f.steady.perAgent)
	fmt.Fprintf(text, "- peak commit: base %.0f MB, %.1f MB per agent\n", f.commit.base, f.commit.perAgent)
	fmt.Fprintf(text, "- CPU: base %.2f s, %.2f s per agent\n", f.cpu.base, f.cpu.perAgent)
	fmt.Fprintf(text, "- cores while running: base %.3f, %.3f per agent; agents that fit in 2 vCPU: %s\n\n", f.load.base, f.load.perAgent, fitsIn(coreBudget, f.load, measuredTo))
}

func knownSection(text *strings.Builder, rows []row) {
	text.WriteString("## The sampler against a known child\n\n")
	text.WriteString("Each row is this binary's own `alloc` verb, which allocates and touches a fixed size, spins one core for a fixed time, starts `kids` children doing the same, and holds for 2 s. Expected is above the matching zero-size row.\n\n")
	text.WriteString("| kids | expected MB | peak RSS MB | above zero row MB | error MB | expected CPU s | CPU s | above zero row s | processes | date |\n")
	text.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		zero := r
		for _, other := range rows {
			if other.Agents == r.Agents && other.ExpectMB == 0 {
				zero = other
			}
		}
		above := r.PeakRSSMB - zero.PeakRSSMB
		fmt.Fprintf(text, "| %d | %.0f | %.1f | %.1f | %+.1f | %.2f | %.2f | %.2f | %d | %s |\n",
			r.Agents, r.ExpectMB, r.PeakRSSMB, above, above-r.ExpectMB, r.ExpectCPUS, r.CPUS, r.CPUS-zero.CPUS, r.PeakProcesses, r.Date)
	}
	text.WriteString("\n")
}

func report(args []string) error {
	if len(args) != 2 {
		return errors.New("report needs the results file and the report path")
	}
	rows, err := readRows(args[0])
	if err != nil {
		return err
	}
	var known []row
	var groups [][]row
	for _, r := range rows {
		if r.Arm == "known" {
			known = append(known, r)
			continue
		}
		at := slices.IndexFunc(groups, func(g []row) bool {
			return g[0].Tool == r.Tool && g[0].Arm == r.Arm && g[0].Credential == r.Credential
		})
		if at < 0 {
			groups = append(groups, nil)
			at = len(groups) - 1
		}
		groups[at] = append(groups[at], r)
	}
	var text strings.Builder
	text.WriteString("# What each extra sub-agent costs in memory and CPU\n\n")
	fmt.Fprintf(&text, "Machine: %s. Sampled every %s. %s. Peak and steady RSS are the maximum and the median of the samples, and a page shared by two processes counts in both working sets. No run was discarded and nothing was warmed up. The live arms need the network and each tool's subscription login; the cassette and known arms need neither.\n\n%s\n\n", machine(), sampleEvery, treeMethod, armConditions)
	summary(&text, groups)
	for _, group := range groups {
		toolSection(&text, group)
	}
	knownSection(&text, known)
	return os.WriteFile(args[1], []byte(text.String()), 0o644)
}
