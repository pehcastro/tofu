package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func render() error {
	files, err := filepath.Glob(filepath.Join(benchDir, "passes", "*.json"))
	if err != nil {
		return err
	}
	var passes []pass
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var p pass
		if err := json.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		passes = append(passes, p)
	}
	slices.SortFunc(passes, func(a, b pass) int { return a.Recorded.Compare(b.Recorded) })
	base := slices.IndexFunc(passes, func(p pass) bool { return p.Name == "v0" })
	if base < 0 {
		return fmt.Errorf("no v0 pass under %s", filepath.Join(benchDir, "passes"))
	}
	var page strings.Builder
	page.WriteString("# CLI passes\n\n")
	page.WriteString("Written by `go run ./bench/cli -pass <name> -note <what changed>` from the repository root, with `-modfile` to build an earlier dependency set. Each pass builds `./cmd/tofu`, then runs every command below in a home and working directory made fresh for each run, with GOMAXPROCS=2, stdin empty and HTTP(S)_PROXY pointed at a dead port so no verb reaches the network. Init is the sum of `GODEBUG=inittrace=1` over `tofu version`. Every figure is the median of the pass's runs, wall also at p90. Each pass is compared with the earlier pass named by `-against`, v0 when none is named, and every ratio is the earlier figure over the pass's.\n\n")
	page.WriteString("## Init\n\n| pass | tofu | commit | recorded | platform | runs | init clock ms | init allocations MB | note |\n|---|---|---|---|---|---|---|---|---|\n")
	var methods []string
	byName := map[string]pass{}
	for _, p := range passes {
		hash, dirty, _ := strings.Cut(p.Commit, "-")
		fmt.Fprintf(&page, "| %s | %s | %s %s | %s | %s | %d | %.1f | %.2f | %s |\n", p.Name, p.Version, hash[:min(8, len(hash))], dirty, p.Recorded.Format(time.DateTime), p.Platform, p.Runs, p.InitMs, p.InitMB, p.Note)
		methods = append(methods, p.Method)
		byName[p.Name] = p
	}
	slices.Sort(methods)
	page.WriteString("\n")
	for _, method := range slices.Compact(methods) {
		fmt.Fprintf(&page, "%s.\n", method)
	}
	for _, p := range passes[base+1:] {
		before, found := byName[cmp.Or(p.Against, "v0")]
		if !found {
			return fmt.Errorf("%s is compared with %s, which has no pass", p.Name, p.Against)
		}
		baseline := map[string]row{}
		for _, r := range before.Rows {
			baseline[r.Command] = r
		}
		b, a := before.Name+" "+before.Version, p.Name+" "+p.Version
		fmt.Fprintf(&page, "\n## %[1]s against %[2]s\n\n| command | %[2]s CPU ms | %[1]s CPU ms | CPU ratio | %[2]s wall ms | %[1]s wall ms | wall ratio | %[1]s wall p90 ms | %[2]s RSS MB | %[1]s RSS MB |\n|---|---|---|---|---|---|---|---|---|---|\n", a, b)
		for _, r := range p.Rows {
			was := baseline[r.Command]
			fmt.Fprintf(&page, "| `%s` | %.1f | %.1f | %.2fx | %.1f | %.1f | %.2fx | %.1f | %.1f | %.1f |\n", r.Command, was.CPUMs, r.CPUMs, was.CPUMs/r.CPUMs, was.WallMs, r.WallMs, was.WallMs/r.WallMs, r.WallP90Ms, was.RSSMB, r.RSSMB)
		}
	}
	measured, err := os.ReadFile(filepath.Join(benchDir, "measured.json"))
	if err != nil {
		return err
	}
	var measures []measure
	if err := json.Unmarshal(measured, &measures); err != nil {
		return fmt.Errorf("measured.json: %w", err)
	}
	page.WriteString("\n## Outside the verbs\n\nChanges a verb timing cannot see, each measured by the command named in its row, before and after, on the same machine. The ratio is before over after where less is better and after over before where more is, so above 1x is a gain and below it a cost.\n\n| pass | tofu | change | measure | before | after | ratio | commit | measured by |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, m := range measures {
		gain := m.Before / m.After
		if m.MoreIsBetter {
			gain = 1 / gain
		}
		fmt.Fprintf(&page, "| %s | %s | %s | %s | %s | %s | %.2fx | %s | `%s` |\n", m.Pass, m.Version, m.Change, m.Measure,
			strconv.FormatFloat(m.Before, 'f', -1, 64), strconv.FormatFloat(m.After, 'f', -1, 64), gain, m.Commit, m.Command)
	}
	fmt.Fprintf(&page, "\n## v0 %s\n\n| command | CPU ms | wall ms | wall p90 ms | RSS MB |\n|---|---|---|---|---|\n", passes[base].Version)
	for _, r := range passes[base].Rows {
		fmt.Fprintf(&page, "| `%s` | %.1f | %.1f | %.1f | %.1f |\n", r.Command, r.CPUMs, r.WallMs, r.WallP90Ms, r.RSSMB)
	}
	return os.WriteFile(filepath.Join(benchDir, "passes.md"), []byte(page.String()), 0o644)
}

type measure struct {
	Pass, Version, Change, Measure, Commit, Command string
	Before, After                                   float64
	MoreIsBetter                                    bool
}
