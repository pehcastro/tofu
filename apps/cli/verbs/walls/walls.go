package walls

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/internal/konst"
	"tofu/internal/platform/fs"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/stat"
	"tofu/internal/store/walls"
)

const Usage = "usage: tofu walls [--since <14d|36h>] [--op <op>] [--own] [--tier <S|A|B|C|D|E>] [--all] [--json]"

type wallRow struct {
	Op   walls.Op `json:"op"`
	Name string   `json:"name"`
	N    int      `json:"n"`
	P50  int64    `json:"p50"`
	P90  int64    `json:"p90"`
	Max  int64    `json:"max"`
	Tier string   `json:"tier,omitempty"`
}

type report struct {
	Since          time.Time `json:"since"`
	Projects       int       `json:"projects"`
	Own            bool      `json:"own"`
	Rows           []wallRow `json:"rows"`
	Bad            int       `json:"bad_lines"`
	Unread         int       `json:"unread_files"`
	WithoutOutside int       `json:"without_outside_ms,omitempty"`
}

type options struct {
	since time.Duration
	op    walls.Op
	all   bool
	own   bool
	tier  string
}

const tierLetters = "SABCDE"

func tierOf(ms int64) string {
	switch {
	case ms < konst.WallTierSUnderMillis:
		return "S"
	case ms < konst.WallTierAUnderMillis:
		return "A"
	case ms < konst.WallTierBUnderMillis:
		return "B"
	case ms < konst.WallTierCUnderMillis:
		return "C"
	case ms < konst.WallTierDUnderMillis:
		return "D"
	}
	return "E"
}

func Verb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "walls", UsageLine: Usage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseOptions(output.WithoutJSON(args))
	if err != nil {
		return o.Usage(err)
	}
	projects, err := projectPlaces(opts.all)
	if err != nil {
		return o.Fail(err)
	}
	read := report{Since: time.Now().Add(-opts.since).UTC(), Projects: len(projects), Own: opts.own}
	var all []walls.Wall
	for _, places := range projects {
		reading, err := walls.ReadProject(places, read.Since)
		if err != nil {
			return o.Fail(fmt.Errorf("%s: %w", places.State, err))
		}
		all, read.Bad, read.Unread = append(all, reading.Walls...), read.Bad+reading.Bad, read.Unread+reading.Unread
	}
	var kept []walls.Wall
	for _, wall := range all {
		switch {
		case opts.op != "" && wall.Op != opts.op:
		case !opts.own:
			kept = append(kept, wall)
		case wall.OutsideMS == nil:
			read.WithoutOutside++
		case wall.MS > *wall.OutsideMS || *wall.OutsideMS == 0:
			wall.MS -= *wall.OutsideMS
			kept = append(kept, wall)
		}
	}
	read.Rows = rowsOf(kept, opts)
	return o.Done(true, read, func(page cli.Page) []string { return wallLines(page, read, opts) })
}

func parseOptions(args []string) (options, error) {
	opts := options{since: konst.WallsWindowDays * 24 * time.Hour}
	for at := 0; at < len(args); at++ {
		flag := args[at]
		switch flag {
		case "--all":
			opts.all = true
			continue
		case "--own":
			opts.own = true
			continue
		}
		if at+1 >= len(args) || !slices.Contains([]string{"--since", "--op", "--tier"}, flag) {
			return opts, fmt.Errorf("unknown argument %q", flag)
		}
		at++
		var err error
		switch flag {
		case "--op":
			opts.op, err = walls.ParseOp(args[at])
		case "--since":
			opts.since, err = window(args[at])
		case "--tier":
			opts.tier, opts.own = args[at], true
			if len(opts.tier) != 1 || !strings.Contains(tierLetters, opts.tier) {
				err = fmt.Errorf("--tier %q is not a tier; one of S, A, B, C, D or E", opts.tier)
			}
		}
		if err != nil {
			return opts, err
		}
	}
	return opts, nil
}

func window(text string) (time.Duration, error) {
	if days, isDays := strings.CutSuffix(text, "d"); isDays {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("--since %q is not a whole number of days", text)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	took, err := time.ParseDuration(text)
	if err != nil || took <= 0 {
		return 0, fmt.Errorf("--since %q is neither days like 14d nor a duration like 36h", text)
	}
	return took, nil
}

func projectPlaces(all bool) ([]platformproject.Places, error) {
	if !all {
		places, err := platformproject.ResolvePlaces(".")
		return []platformproject.Places{places}, err
	}
	home, err := fs.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(home, fs.ProjectsDirName))
	var projects []platformproject.Places
	for _, entry := range entries {
		if state := filepath.Join(home, fs.ProjectsDirName, entry.Name()); entry.IsDir() {
			projects = append(projects, platformproject.Places{State: state, Log: filepath.Join(state, "log")})
		}
	}
	return projects, err
}

func rowsOf(all []walls.Wall, opts options) []wallRow {
	type key struct {
		op   walls.Op
		name string
	}
	grouped := map[key][]float64{}
	for _, wall := range all {
		at := key{wall.Op, wall.Name}
		grouped[at] = append(grouped[at], float64(wall.MS))
	}
	rows := make([]wallRow, 0, len(grouped))
	for at, ms := range grouped {
		rounded := func(p float64) int64 { return int64(math.Round(stat.Percentile(ms, p))) }
		row := wallRow{Op: at.op, Name: at.name, N: len(ms), P50: rounded(50), P90: rounded(90), Max: int64(slices.Max(ms))}
		if opts.own {
			row.Tier = tierOf(row.P50)
		}
		if opts.tier == "" || row.Tier == opts.tier {
			rows = append(rows, row)
		}
	}
	slices.SortFunc(rows, func(a, b wallRow) int {
		return cmp.Or(cmp.Compare(slices.Index(walls.Ops(), a.Op), slices.Index(walls.Ops(), b.Op)), cmp.Compare(b.N, a.N), strings.Compare(a.Name, b.Name))
	})
	return rows
}

func wallLines(page cli.Page, read report, opts options) []string {
	facts := []string{"since " + read.Since.Local().Format(time.DateTime), output.Plural(len(read.Rows), "row")}
	if opts.all {
		facts = append(facts, output.Plural(read.Projects, "project"))
	}
	if opts.own {
		facts = append(facts, "own ms: ms minus outside_ms")
	}
	if read.Bad+read.Unread > 0 {
		facts = append(facts, output.Plural(read.Bad, "line")+" and "+output.Plural(read.Unread, "file")+" not read")
	}
	var footer []string
	if read.WithoutOutside > 0 {
		footer = append([]string{""}, cli.Indent(output.Plural(read.WithoutOutside, "wall")+" recorded without outside_ms left out")...)
	}
	if len(read.Rows) == 0 {
		return append(page.Title("Walls", facts, cli.Verdict{Mark: cli.Idle, Text: "nothing timed in the window"}), footer...)
	}
	header := []string{"op", "name", "n", "p50 ms", "p90 ms", "max ms"}
	if opts.own {
		header = append(header, "tier")
	}
	rows := []cli.Row{{Cells: header}}
	for _, row := range read.Rows {
		cells := []string{string(row.Op), row.Name, strconv.Itoa(row.N), strconv.FormatInt(row.P50, 10), strconv.FormatInt(row.P90, 10), strconv.FormatInt(row.Max, 10)}
		if opts.own {
			cells = append(cells, row.Tier)
		}
		rows = append(rows, cli.Row{Cells: cells})
	}
	return append(append(append(page.Title("Walls", facts, cli.Verdict{}), ""), cli.Indent(page.Rows(rows)...)...), footer...)
}
