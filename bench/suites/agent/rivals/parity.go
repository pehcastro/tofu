package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
)

type pairing struct {
	label, arm, setting string
}

var parityPairs = []pairing{
	{"tofu-sonnet at Claude Code's setting", "tofu-sonnet", thinkingOmitted},
	{"Claude Code", "claude-sonnet", ""},
	{"tofu-sonnet at omp's setting", "tofu-sonnet", thinkingOmitted},
	{"omp", "omp-sonnet", ""},
	{"tofu-gpt at codex's setting", "tofu-gpt", thinkingOmitted},
	{"codex", "codex-gpt", ""},
}

type pairRows struct {
	label string
	rows  []row
}

type column struct {
	head  string
	value func(row) float64
}

func tenths(v float64) float64 { return math.Round(v*10) / 10 }

func parityColumns(outOf int, unit string, perUnit float64) []column {
	return []column{
		{fmt.Sprintf("quality of %d", outOf), func(r row) float64 { got, _ := r.Score.quality(); return float64(got) }},
		{"wall s", func(r row) float64 { return tenths(seconds(r.WallMS)) }},
		{"conversation requests", func(r row) float64 { return float64(r.Requests) }},
		{"memory or judge calls", func(r row) float64 { return float64(r.Auxiliary) }},
		{"total in", func(r row) float64 { return float64(r.Tokens.in()) }},
		{"out", func(r row) float64 { return float64(r.Tokens.Out) }},
		{"peak tree " + unit, func(r row) float64 { return tenths(float64(r.Tree.PeakTreeRSS) / perUnit) }},
		{"CPU s", func(r row) float64 { return tenths(seconds(r.Tree.CPUMS)) }},
		{"failed calls", func(r row) float64 { return float64(failedCalls(r)) }},
	}
}

func printParity(args []string) error {
	flags := flag.NewFlagSet("parity", flag.ContinueOnError)
	batch := flags.String("batch", "", "folder under runs that holds the batch")
	taskName := flags.String("task", "", "task folder inside the batch; empty is the first task")
	rivals := flags.String("rivals", "", "folder under runs whose rows of the rivals join the table; empty is the batch, . is runs itself")
	unit := flags.String("unit", "MB", "peak tree unit, MB for 10^6 bytes or MiB for 2^20")
	if err := flags.Parse(args); err != nil {
		return err
	}
	perUnit := map[string]float64{"MB": 1e6, "MiB": 1 << 20}[*unit]
	if *batch == "" || perUnit == 0 {
		return errors.New("parity needs -batch, and -unit is MB or MiB")
	}
	rows, err := parityRows(*batch, *taskName, *rivals)
	if err != nil {
		return err
	}
	_, outOf := rows[0].Score.quality()
	columns := parityColumns(outOf, *unit, perUnit)
	heads := make([]string, 0, len(columns))
	for _, c := range columns {
		heads = append(heads, c.head)
	}
	fmt.Printf("| arm | version | runs | %s |\n|%s\n", strings.Join(heads, " | "), strings.Repeat("---|", len(columns)+3))
	var groups []pairRows
	for _, p := range parityPairs {
		mine := slices.DeleteFunc(slices.Clone(rows), func(r row) bool { return r.Arm != p.arm || r.Setting != p.setting })
		if len(mine) == 0 {
			continue
		}
		groups = append(groups, pairRows{p.label, mine})
		cells := []string{p.label, distinct(mine, func(r row) string { return r.Tool + " " + r.Version }), fmt.Sprint(len(mine))}
		for _, c := range columns {
			cells = append(cells, spread(mine, c.value))
		}
		fmt.Println("| " + strings.Join(cells, " | ") + " |")
	}
	fmt.Printf("\n| run | tool and version | date | thinking sent | %s |\n|%s\n", strings.Join(heads, " | "), strings.Repeat("---|", len(columns)+4))
	for _, r := range rows {
		cells := []string{r.name(), r.Tool + " " + r.Version, r.Date, cmp.Or(r.ThinkingSent, "not read")}
		for _, c := range columns {
			cells = append(cells, trim(c.value(r)))
		}
		fmt.Println("| " + strings.Join(cells, " | ") + " |")
	}
	if *taskName == "" {
		printEditTimes(groups)
	}
	return nil
}

func parityRows(batch, taskName, rivals string) ([]row, error) {
	rows, err := readRows(filepath.Join(here(), "runs", batch, taskName, "*.json"))
	if err != nil {
		return nil, err
	}
	if rivals != "" {
		theirs, err := readRows(filepath.Join(here(), "runs", rivals, taskName, "*.json"))
		if err != nil {
			return nil, err
		}
		rows = append(rows, slices.DeleteFunc(theirs, func(r row) bool { return r.Tool == "tofu" })...)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows in runs/%s/%s", batch, taskName)
	}
	slices.SortFunc(rows, func(x, y row) int { return cmp.Or(cmp.Compare(x.Date, y.Date), cmp.Compare(x.Arm, y.Arm)) })
	return rows, nil
}

func printEditTimes(groups []pairRows) {
	fmt.Print("\n| arm | edit calls | p50 ms |\n|---|---|---|\n")
	for _, g := range groups {
		var times []int64
		count := 0
		for _, r := range g.rows {
			for _, c := range r.Calls {
				if c.Kind == "edit" {
					count++
					if c.MS >= 0 {
						times = append(times, c.MS)
					}
				}
			}
		}
		slices.Sort(times)
		fmt.Printf("| %s | %d | %s |\n", g.label, count, rank(times, 0.5))
	}
}
