package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
)

type group struct {
	kind, arm string
	frames    []float64
	readies   []float64
	failed    int
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	return sorted[max(0, int(math.Ceil(q*float64(len(sorted))))-1)]
}

func readRows(paths []string) ([]row, error) {
	var rows []row
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for line := range strings.Lines(string(body)) {
			var r row
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func report(args []string) error {
	flags := flag.NewFlagSet("report", flag.ExitOnError)
	baseline := flags.String("baseline", "", "the arm every other arm of the same case is compared with")
	_ = flags.Parse(args)
	rows, err := readRows(flags.Args())
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("report needs at least one jsonl with rows")
	}
	credentials := map[string]bool{}
	var groups []*group
	for _, r := range rows {
		credentials[r.Credential] = true
		at := slices.IndexFunc(groups, func(g *group) bool { return g.kind == r.Case && g.arm == r.Arm })
		if at < 0 {
			groups, at = append(groups, &group{kind: r.Case, arm: r.Arm}), len(groups)
		}
		if r.Error != "" {
			groups[at].failed++
			continue
		}
		groups[at].frames, groups[at].readies = append(groups[at].frames, r.FrameMS), append(groups[at].readies, r.ReadyMS)
	}
	if len(credentials) > 1 {
		return fmt.Errorf("rows from %d credential kinds are not comparable, report them apart", len(credentials))
	}
	first, last := rows[0], rows[len(rows)-1]
	fmt.Printf("Conditions: tofu %s at %s, %s, %s, %s, %s to %s.\n\n", first.Version, first.Commit, first.Machine, first.Credential, first.Wire, first.Date, last.Date)
	fmt.Println("| case | arm | runs | failed | first frame p50 ms | p90 | min | max | accepts typing p50 ms | p90 |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|")
	for _, g := range groups {
		slices.Sort(g.frames)
		slices.Sort(g.readies)
		fmt.Printf("| %s | %s | %d | %d | %.0f | %.0f | %.0f | %.0f | %.0f | %.0f |\n", g.kind, g.arm, len(g.frames), g.failed,
			percentile(g.frames, 0.5), percentile(g.frames, 0.9), percentile(g.frames, 0), percentile(g.frames, 1),
			percentile(g.readies, 0.5), percentile(g.readies, 0.9))
	}
	if *baseline == "" {
		return nil
	}
	fmt.Printf("\n| case | arm | first frame p50 ms | against %s | saved ms | speed-up |\n|---|---|---|---|---|---|\n", *baseline)
	for _, g := range groups {
		at := slices.IndexFunc(groups, func(b *group) bool { return b.kind == g.kind && b.arm == *baseline })
		if at < 0 || groups[at] == g {
			continue
		}
		base := groups[at]
		mine, theirs := percentile(g.frames, 0.5), percentile(base.frames, 0.5)
		fmt.Printf("| %s | %s | %.0f | %.0f | %.0f | %.2fx |\n", g.kind, g.arm, mine, theirs, theirs-mine, theirs/mine)
	}
	return nil
}
