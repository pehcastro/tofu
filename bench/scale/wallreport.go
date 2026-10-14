package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

const (
	wallConditions = "Each row is the bench/scale tiny task driven by `wall.sh`: the lead spawns N sub-agents, each reads one file and answers its first line, and the lead prints them, under `tofu run --no-instructions --no-gate --sift free`. The cassette arm runs in a HOME made fresh for each run on scripted replies, so its model column is the cassette's own pacing and every other column is tofu. The live arm runs claude-sub/claude-sonnet-5-5 on the owner's subscription in his own HOME, because the subscription login lives there; it needs the network and is skipped without it. No run was discarded and nothing was warmed up."
	wallSteps      = "Per sub-agent, from the end of the lead request that asked for the spawn to the end of the lead turn that read its report, the steps add up to read. notice is the spawn tool's own steps as its timing notice reports them; wait is the rest until the sub-agent's run is recorded as begun, which holds a sibling of one definition until the first sibling's first request returns; start is that to its first model request; model is the sum of its request durations; tools the sum of its tool calls; between is the rest of its own run, tofu's work between steps; gather is its end to the lead taking its report, where the 1.5 s quiet and 10 s cap of the report gathering sit; lead is the lead turn that read it. Per run: startup is process start to the lead's first request, ask is the request that spawned, fan is that request's end to the final answer, memory is the memory model's requests after it, exit is the rest of the tail. Every cell is a median in seconds, per sub-agent over every sub-agent of every run, per run over runs; read max is the slowest sub-agent."
)

type wallColumn struct {
	name  string
	value func(wallRow) float64
}

var agentColumns = []wallColumn{
	{"notice", func(r wallRow) float64 { return r.NoticeParts["total"] }},
	{"wait", func(r wallRow) float64 { return r.SpawnMS - r.NoticeParts["total"] }},
	{"start", func(r wallRow) float64 { return r.StartMS }},
	{"model", func(r wallRow) float64 { return r.ModelMS }},
	{"tools", func(r wallRow) float64 { return r.ToolsMS }},
	{"between", func(r wallRow) float64 { return r.BetweenMS }},
	{"gather", func(r wallRow) float64 { return r.GatherMS }},
	{"lead", func(r wallRow) float64 { return r.LeadMS }},
}

var runColumns = []wallColumn{
	{"startup", func(r wallRow) float64 { return r.StartupMS }},
	{"ask", func(r wallRow) float64 { return r.AskMS }},
	{"fan", func(r wallRow) float64 { return r.FanMS }},
	{"memory", func(r wallRow) float64 { return r.MemoryMS }},
	{"exit", func(r wallRow) float64 { return r.TailMS - r.MemoryMS }},
	{"wall", func(r wallRow) float64 { return r.WallMS }},
	{"lead turns", func(r wallRow) float64 { return 1000 * float64(r.LeadTurns) }},
}

type wallGroup struct {
	first          wallRow
	runs, spawned  []wallRow
	answered, want int
}

func median(rows []wallRow, value func(wallRow) float64) float64 {
	var values []float64
	for _, r := range rows {
		values = append(values, value(r))
	}
	slices.Sort(values)
	return values[len(values)/2]
}

func header(text *strings.Builder, lead string, columns []wallColumn, tail string) {
	text.WriteString("| " + lead + " |")
	for _, column := range columns {
		text.WriteString(" " + column.name + " |")
	}
	text.WriteString(tail + "\n|" + strings.Repeat("---|", strings.Count(lead, "|")+1+len(columns)+strings.Count(tail, "|")) + "\n")
}

func readWallRows(path string) ([][]wallRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var groups [][]wallRow
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var r wallRow
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return nil, err
		}
		at := slices.IndexFunc(groups, func(g []wallRow) bool { return g[0].Arm == r.Arm && g[0].Agents == r.Agents && g[0].Commit == r.Commit })
		if at < 0 {
			groups, at = append(groups, nil), len(groups)
		}
		groups[at] = append(groups[at], r)
	}
	return groups, scanner.Err()
}

func grouped(all []wallRow) wallGroup {
	g := wallGroup{first: all[0], runs: slices.CompactFunc(slices.Clone(all), func(a, b wallRow) bool { return a.Run == b.Run })}
	g.spawned = slices.DeleteFunc(slices.Clone(all), func(r wallRow) bool { return r.Agent == "" })
	for _, r := range g.runs {
		g.answered, g.want = g.answered+r.Answered, g.want+r.Agents
	}
	return g
}

func wallreport(args []string) error {
	if len(args) != 3 {
		return errors.New("wallreport needs the wall rows, the report path and the notes appended to it")
	}
	notes, err := os.ReadFile(args[2])
	if err != nil {
		return err
	}
	rows, err := readWallRows(args[0])
	if err != nil {
		return err
	}
	var groups []wallGroup
	for _, all := range rows {
		groups = append(groups, grouped(all))
	}
	var text strings.Builder
	fmt.Fprintf(&text, "# Where a sub-agent's wall time goes\n\nMachine: %s. %s\n\n%s\n\n## Per sub-agent\n\n", machine(), wallConditions, wallSteps)
	header(&text, "arm | agents | runs | version | model", agentColumns, " requests | read | read max | answered |")
	for _, g := range groups {
		fmt.Fprintf(&text, "| %s | %d | %d | %s %.8s | %s |", g.first.Arm, g.first.Agents, len(g.runs), g.first.Version, g.first.Commit, g.first.Model)
		if len(g.spawned) == 0 {
			fmt.Fprintf(&text, "%s %d/%d |\n", strings.Repeat(" no sub-agent ran |", len(agentColumns)+3), g.answered, g.want)
			continue
		}
		for _, column := range agentColumns {
			fmt.Fprintf(&text, " %.2f |", median(g.spawned, column.value)/1000)
		}
		slowest := slices.MaxFunc(g.spawned, func(a, b wallRow) int { return int(a.ReadMS - b.ReadMS) })
		fmt.Fprintf(&text, " %.0f | %.2f | %.2f | %d/%d |\n", median(g.spawned, func(r wallRow) float64 { return float64(r.Requests) }),
			median(g.spawned, func(r wallRow) float64 { return r.ReadMS })/1000, slowest.ReadMS/1000, g.answered, g.want)
	}
	text.WriteString("\n## Per run\n\n")
	header(&text, "arm | agents", runColumns, "")
	for _, g := range groups {
		fmt.Fprintf(&text, "| %s | %d |", g.first.Arm, g.first.Agents)
		for _, column := range runColumns {
			fmt.Fprintf(&text, " %.2f |", median(g.runs, column.value)/1000)
		}
		text.WriteString("\n")
	}
	text.WriteString("\n## Share of read, by step\n\n")
	header(&text, "arm | agents", agentColumns, "")
	for _, g := range groups {
		if len(g.spawned) == 0 {
			continue
		}
		var whole float64
		for _, r := range g.spawned {
			whole += r.ReadMS
		}
		fmt.Fprintf(&text, "| %s | %d |", g.first.Arm, g.first.Agents)
		for _, column := range agentColumns {
			var part float64
			for _, r := range g.spawned {
				part += column.value(r)
			}
			fmt.Fprintf(&text, " %.0f%% |", 100*part/whole)
		}
		text.WriteString("\n")
	}
	text.WriteString("\n## The spawn notice's own steps, median ms\n\n")
	var parts []wallColumn
	for _, g := range groups {
		for _, r := range g.spawned {
			for _, name := range slices.Sorted(maps.Keys(r.NoticeParts)) {
				if !slices.ContainsFunc(parts, func(c wallColumn) bool { return c.name == name }) {
					parts = append(parts, wallColumn{name, func(r wallRow) float64 { return r.NoticeParts[name] }})
				}
			}
		}
	}
	header(&text, "arm | agents", parts, "")
	for _, g := range groups {
		if len(g.spawned) == 0 {
			continue
		}
		fmt.Fprintf(&text, "| %s | %d |", g.first.Arm, g.first.Agents)
		for _, part := range parts {
			fmt.Fprintf(&text, " %.0f |", median(g.spawned, part.value))
		}
		text.WriteString("\n")
	}
	text.WriteString("\n")
	text.Write(notes)
	return os.WriteFile(args[1], []byte(text.String()), 0o644)
}
