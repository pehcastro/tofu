package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	cli "tofu/apps/cli/page"
	"tofu/internal/cli/render"
	"tofu/internal/store/session/cost"
)

const (
	boardyCostUsage = "tofu boardy cost <ticket> --claude | --session <id> [--fork N] --claude [--json]"
	shortID         = 8
	msPerSecond     = float64(time.Second / time.Millisecond)
)

func (c boardyCall) cost() int {
	if c.flags["claude"] != "true" {
		return c.o.Usage(errors.New("name the source with --claude: the tofu trace reader is not written yet"))
	}
	session, ticket := c.flags["session"], ""
	switch {
	case session != "" && len(c.args) == 0:
	case session == "" && len(c.args) == 1 && c.flags["fork"] == "":
		ticket = c.args[0]
	default:
		return c.o.Usage(errors.New("name one " + c.words.Ticket + ", or --session <id>"))
	}
	table, title, err := c.costTable(session, ticket)
	if err != nil {
		return c.o.Fail(err)
	}
	return c.o.Done(true, table, func(page cli.Page) []string { return costLines(page, title, table, ticket == "") })
}

func (c boardyCall) costTable(session, ticket string) (cost.Table, string, error) {
	home, err := os.UserHomeDir()
	home = cmp.Or(os.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude"))
	if _, statErr := os.Stat(home); err != nil || statErr != nil {
		return cost.Table{}, "", fmt.Errorf("no Claude Code directory at %s", home)
	}
	boards, err := c.store.Boards()
	if err != nil {
		return cost.Table{}, "", err
	}
	keys := make([]string, len(boards))
	for i, board := range boards {
		keys[i] = board.Key
	}
	root := cost.ProjectDir(home, c.dir)
	if ticket != "" {
		if _, err := c.managed.Get(ticket); err != nil {
			return cost.Table{}, "", err
		}
		book, err := cost.Project(root, keys)
		return book.ForTicket(ticket), ticket + " cost", err
	}
	book, err := cost.Session(root, session, keys)
	if err != nil {
		return cost.Table{}, "", err
	}
	title := "session " + session + " cost"
	if fork := c.flags["fork"]; fork != "" {
		n, err := strconv.Atoi(fork)
		if err != nil || n < 0 {
			return cost.Table{}, "", fmt.Errorf("--fork %q is not a compaction number", fork)
		}
		book, title = book.Fork(n), title+" fork "+fork
	}
	return book.BySession(), title, nil
}

func costLines(page cli.Page, title string, table cost.Table, bySession bool) []string {
	rows := append(slices.Clone(table.Rows), table.Total)
	label := func(row cost.Row) string {
		switch {
		case row.Key == cost.TotalRow || bySession:
			return row.Key
		case row.Agent == "":
			return cost.LeadRow + " " + short(row.Session)
		}
		return row.AgentType + " " + short(row.Agent)
	}
	busy, share := make([]float64, len(table.Rows)), make([]float64, len(table.Rows))
	for i, row := range table.Rows {
		busy[i], share[i] = float64(row.BusyMs), row.ShareMs
	}
	busySeconds, shareSeconds := apportion(busy), apportion(share)
	section := func(name string, head []string, cells func(i int, row cost.Row) []string) []string {
		grid := [][]string{append([]string{name}, head...)}
		for i, row := range rows {
			grid = append(grid, append([]string{label(row)}, cells(i, row)...))
		}
		widths := make([]int, len(grid[0]))
		for _, line := range grid {
			for i, cell := range line {
				widths[i] = max(widths[i], render.Cells(cell))
			}
		}
		out := make([]cli.Row, len(grid))
		for at, line := range grid {
			aligned := []string{render.Pad(line[0], widths[0])}
			for i, cell := range line[1:] {
				aligned = append(aligned, render.Lead(cell, widths[i+1]))
			}
			out[at] = cli.Row{Cells: aligned}
		}
		return append(page.Rows(out), "")
	}
	n := strconv.Itoa
	lines := page.Title(title, []string{"claude code transcripts", n(len(table.Rows)) + " rows"}, cli.Verdict{})
	lines = append(lines, "")
	lines = append(lines, section("time", []string{"wall", "wall share", "busy", "model", "thinking"}, func(i int, row cost.Row) []string {
		return []string{clock(row.WallMs), seconds(shareSeconds[i]), seconds(busySeconds[i]), clock(row.ModelMs), clock(row.ThinkMs)}
	})...)
	lines = append(lines, section("tokens", []string{"requests", "in", "out", "cache rd", "cache wr", "thinking"}, func(_ int, row cost.Row) []string {
		t := row.Tokens
		return []string{n(row.Requests), i64(t.In), i64(t.Out), i64(t.CacheRead), i64(t.CacheWrite), i64(t.Thinking)}
	})...)
	lines = append(lines, section("runs", []string{"builds", "failed", "build time", "tests", "failed", "test time"}, func(_ int, row cost.Row) []string {
		return []string{n(row.Builds.Count), n(row.Builds.Failed), clock(row.Builds.Ms), n(row.Tests.Count), n(row.Tests.Failed), clock(row.Tests.Ms)}
	})...)
	lines = append(lines, section("work", []string{"iterations", "reads", "read time", "spawns", "resumes"}, func(_ int, row cost.Row) []string {
		return []string{n(row.TestIterations), n(row.Reads.Count), clock(row.Reads.Ms), n(row.Spawns), n(row.Resumes)}
	})...)
	lines = append(lines, section("files", []string{"created", "edited", "lines added", "lines removed"}, func(_ int, row cost.Row) []string {
		w := row.Writes
		return []string{n(w.FilesCreated), n(w.FilesEdited), n(w.LinesAdded), n(w.LinesRemoved)}
	})...)
	return append(lines, page.Label("inferred: tickets from Agent prompts, resumes and boardy commands; tool time from line timestamps, permission prompts included; gaps over "+render.Until(cost.IdleCap)+" are idle"))
}

func apportion(ms []float64) []int64 {
	var total float64
	whole := make([]int64, len(ms)+1)
	for i, value := range ms {
		whole[i], total = int64(math.Floor(value/msPerSecond)), total+value
	}
	whole[len(ms)] = int64(math.Round(total / msPerSecond))
	left := whole[len(ms)]
	for _, w := range whole[:len(ms)] {
		left -= w
	}
	order := make([]int, len(ms))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(math.Mod(ms[b], msPerSecond), math.Mod(ms[a], msPerSecond))
	})
	for _, i := range order[:min(max(left, 0), int64(len(ms)))] {
		whole[i]++
	}
	return whole
}

func clock(ms int64) string { return seconds(int64(math.Round(float64(ms) / msPerSecond))) }

func seconds(total int64) string {
	d := time.Duration(total) * time.Second
	return fmt.Sprintf("%d:%02d:%02d", d/time.Hour, d%time.Hour/time.Minute, d%time.Minute/time.Second)
}

func i64(v int64) string { return strconv.FormatInt(v, 10) }

func short(id string) string { return id[:min(shortID, len(id))] }
