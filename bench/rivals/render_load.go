package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	ladderFile = "bench/scale/results.jsonl"
	crowdFile  = "bench/longsession/results.jsonl"
)

type rung struct {
	Tool, Arm, Model, Version, Date                   string
	Agents, Answered                                  int
	WallS, CPUS, PeakRSSMB, SteadyRSSMB, PeakCommitMB float64
}

type crowdSample struct {
	Arm       string  `json:"arm"`
	Phase     string  `json:"phase"`
	Agents    int     `json:"agents"`
	WorkingMB float64 `json:"working_mb"`
	PrivateMB float64 `json:"private_mb"`
	Cores     float64 `json:"cores"`
	Version   string  `json:"version"`
	Wire      string  `json:"wire"`
	Date      string  `json:"date"`
}

type ladder struct {
	tool           string
	rungs          []rung
	perAgent       float64
	steadyPerAgent float64
}

type load struct {
	ladders []ladder
	crowd   []crowdSample
}

func readLines[T any](path string) ([]T, error) {
	file, err := os.Open(filepath.Join(here(), "..", "..", filepath.FromSlash(path)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var out []T
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 1<<20), 16<<20)
	for lines.Scan() {
		var one T
		if err := json.Unmarshal(lines.Bytes(), &one); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, one)
	}
	return out, lines.Err()
}

func readLoad() (load, error) {
	rungs, err := readLines[rung](ladderFile)
	if err != nil {
		return load{}, err
	}
	crowd, err := readLines[crowdSample](crowdFile)
	if err != nil {
		return load{}, err
	}
	var l load
	for _, tool := range []string{"tofu", "claude", "omp", "codex"} {
		mine := slices.DeleteFunc(slices.Clone(rungs), func(r rung) bool { return r.Tool != tool || r.Arm != "live" })
		if len(mine) == 0 {
			continue
		}
		slices.SortFunc(mine, func(x, y rung) int { return x.Agents - y.Agents })
		l.ladders = append(l.ladders, ladder{tool, mine, slope(mine, func(r rung) float64 { return r.PeakRSSMB }), slope(mine, func(r rung) float64 { return r.SteadyRSSMB })})
	}
	l.crowd = slices.DeleteFunc(crowd, func(c crowdSample) bool { return c.Arm != "head10" && c.Arm != "head450" || c.Phase == "seed" })
	return l, nil
}

func slope(rungs []rung, value func(rung) float64) float64 {
	var sumX, sumY float64
	for _, r := range rungs {
		sumX, sumY = sumX+float64(r.Agents), sumY+value(r)
	}
	meanX, meanY := sumX/float64(len(rungs)), sumY/float64(len(rungs))
	var covariance, variance float64
	for _, r := range rungs {
		covariance += (float64(r.Agents) - meanX) * (value(r) - meanY)
		variance += (float64(r.Agents) - meanX) * (float64(r.Agents) - meanX)
	}
	return covariance / variance
}

func (l ladder) top() rung {
	return l.rungs[len(l.rungs)-1]
}

func (l ladder) label() string {
	top := l.top()
	return fmt.Sprintf("%s, %s", toolName(l.tool), top.Model)
}

type crowdCell struct {
	phase                   string
	agents, samples         int
	working, private, cores float64
}

func (l load) crowdAt(arm, phase string) crowdCell {
	cell := crowdCell{phase: phase}
	var working, private, cores []float64
	for _, c := range l.crowd {
		if c.Arm == arm && c.Phase == phase {
			cell.agents = c.Agents
			working, private, cores = append(working, c.WorkingMB), append(private, c.PrivateMB), append(cores, c.Cores)
		}
	}
	cell.samples, cell.working, cell.private, cell.cores = len(working), median(working), median(private), median(cores)
	return cell
}

func loadMarkdown(rep report) string {
	l := rep.load
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Load: memory and CPU as sub-agents are added\n\nFrom `%s`, live arm: one run per rung, each sub-agent reads one file and repeats its first line. MiB per sub-agent is the least squares slope over every rung.\n\n", ladderFile)
	b.WriteString("| tool | version | dates | rungs | peak RSS at 0, MiB | peak RSS at the top rung, MiB | MiB per sub-agent, peak | MiB per sub-agent, steady | peak commit at the top rung, MiB | CPU at the top rung, s | cores at the top rung |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, one := range l.ladders {
		first, top := one.rungs[0], one.top()
		fmt.Fprintf(&b, "| %s | %s | %s | %d to %d sub-agents | %.0f | %.0f | %.1f | %.1f | %.0f | %.1f | %.2f |\n", one.label(), cmp.Or(top.Version, "not recorded"), top.Date, first.Agents, top.Agents,
			first.PeakRSSMB, top.PeakRSSMB, one.perAgent, one.steadyPerAgent, top.PeakCommitMB, top.CPUS, top.CPUS/top.WallS)
	}
	cells := []crowdCell{l.crowdAt("head10", "idle"), l.crowdAt("head450", "idle"), l.crowdAt("head10", "work"), l.crowdAt("head450", "work")}
	fmt.Fprintf(&b, "\n### tofu with %d and with %d sub-agents in one session\n\nFrom `%s`, arms head10 and head450, tofu %s on %s, %s: each cell is the median of the samples of that phase. Idle is the session open with nothing running; work is its sub-agents working.\n\n", cells[0].agents, cells[1].agents, crowdFile, l.crowd[0].Version, l.crowd[0].Wire, l.crowd[0].Date[:10])
	b.WriteString("| sub-agents | phase | samples | working set MiB | private MiB | cores |\n|---|---|---|---|---|---|\n")
	for _, cell := range cells {
		fmt.Fprintf(&b, "| %d | %s | %d | %.0f | %.0f | %.3f |\n", cell.agents, cell.phase, cell.samples, cell.working, cell.private, cell.cores)
	}
	added := float64(cells[1].agents - cells[0].agents)
	fmt.Fprintf(&b, "\nFrom %d to %d sub-agents the working set grows %.2f MiB per sub-agent idle and %.2f MiB working.\n", cells[0].agents, cells[1].agents, (cells[1].working-cells[0].working)/added, (cells[3].working-cells[2].working)/added)
	b.WriteString(subagentTable(rep))
	return b.String()
}

func subagentTable(rep report) string {
	var b strings.Builder
	b.WriteString("\n### Memory per sub-agent on the subagents task\n\nFrom `runs/subagents`: peak tree RSS of a run divided by the sub-agents it started, on a real task with a live model, so the lead's own memory is spread across them.\n\n| arm | build | runs | sub-agents | peak memory, MiB | MiB per sub-agent |\n|---|---|---|---|---|---|\n")
	peak := measures[slices.IndexFunc(measures, func(m measure) bool { return m.name == "peak memory" })]
	agents := measure{"sub-agents", "", "fewer", "more", 0, false, always(func(r row) float64 { return float64(r.Agents) })}
	for _, a := range present(rep.subagents) {
		rows := rep.subagents[a.Name]
		s, _ := statOf(rows, perAgent)
		p, _ := statOf(rows, peak)
		n, _ := statOf(rows, agents)
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s |\n", a.Name, distinct(rows, row.build), len(rows), agents.spread(n), peak.spread(p), perAgent.spread(s))
	}
	return b.String()
}
