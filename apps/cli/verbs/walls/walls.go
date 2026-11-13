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

const Usage = "usage: tofu walls [--since <14d|36h>] [--op <op>] [--all] [--json]"

type wallRow struct {
	Op   walls.Op `json:"op"`
	Name string   `json:"name"`
	N    int      `json:"n"`
	P50  int64    `json:"p50"`
	P90  int64    `json:"p90"`
	Max  int64    `json:"max"`
}

type report struct {
	Since    time.Time `json:"since"`
	Projects int       `json:"projects"`
	Rows     []wallRow `json:"rows"`
	Bad      int       `json:"bad_lines"`
	Unread   int       `json:"unread_files"`
}

type options struct {
	since time.Duration
	op    walls.Op
	all   bool
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
	read := report{Since: time.Now().Add(-opts.since).UTC(), Projects: len(projects)}
	var all []walls.Wall
	for _, places := range projects {
		reading, err := walls.ReadProject(places, read.Since)
		if err != nil {
			return o.Fail(fmt.Errorf("%s: %w", places.State, err))
		}
		all, read.Bad, read.Unread = append(all, reading.Walls...), read.Bad+reading.Bad, read.Unread+reading.Unread
	}
	if opts.op != "" {
		all = slices.DeleteFunc(all, func(wall walls.Wall) bool { return wall.Op != opts.op })
	}
	read.Rows = rowsOf(all)
	return o.Done(true, read, func(page cli.Page) []string { return wallLines(page, read, opts) })
}

func parseOptions(args []string) (options, error) {
	opts := options{since: konst.WallsWindowDays * 24 * time.Hour}
	for at := 0; at < len(args); at++ {
		flag := args[at]
		if flag == "--all" {
			opts.all = true
			continue
		}
		if at+1 >= len(args) || flag != "--since" && flag != "--op" {
			return opts, fmt.Errorf("unknown argument %q", flag)
		}
		at++
		var err error
		if flag == "--op" {
			opts.op, err = walls.ParseOp(args[at])
		} else {
			opts.since, err = window(args[at])
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

func rowsOf(all []walls.Wall) []wallRow {
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
		rows = append(rows, wallRow{Op: at.op, Name: at.name, N: len(ms), P50: rounded(50), P90: rounded(90), Max: int64(slices.Max(ms))})
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
	if read.Bad+read.Unread > 0 {
		facts = append(facts, output.Plural(read.Bad, "line")+" and "+output.Plural(read.Unread, "file")+" not read")
	}
	if len(read.Rows) == 0 {
		return page.Title("Walls", facts, cli.Verdict{Mark: cli.Idle, Text: "nothing timed in the window"})
	}
	rows := []cli.Row{{Cells: []string{"op", "name", "n", "p50 ms", "p90 ms", "max ms"}}}
	for _, row := range read.Rows {
		rows = append(rows, cli.Row{Cells: []string{string(row.Op), row.Name, strconv.Itoa(row.N),
			strconv.FormatInt(row.P50, 10), strconv.FormatInt(row.P90, 10), strconv.FormatInt(row.Max, 10)}})
	}
	return append(append(page.Title("Walls", facts, cli.Verdict{}), ""), cli.Indent(page.Rows(rows)...)...)
}
