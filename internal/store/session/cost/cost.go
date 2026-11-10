package cost

import (
	"cmp"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	LeadRow       = "lead"
	UnassignedRow = "unassigned"
	TotalRow      = "total"
	IdleCap       = 5 * time.Minute
	idleCapMs     = int64(IdleCap / time.Millisecond)
)

type Tokens struct {
	In         int64 `json:"in"`
	Out        int64 `json:"out"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Thinking   int64 `json:"thinking"`
}

type Runs struct {
	Count  int   `json:"count"`
	Ms     int64 `json:"ms"`
	Failed int   `json:"failed"`
}

type Writes struct {
	FilesCreated int `json:"files_created"`
	FilesEdited  int `json:"files_edited"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

type Figures struct {
	WallMs         int64   `json:"wall_ms"`
	ShareMs        float64 `json:"wall_share_ms"`
	BusyMs         int64   `json:"busy_ms"`
	ModelMs        int64   `json:"model_ms"`
	ThinkMs        int64   `json:"thinking_ms"`
	Requests       int     `json:"requests"`
	Tokens         Tokens  `json:"tokens"`
	Builds         Runs    `json:"builds"`
	Tests          Runs    `json:"tests"`
	TestIterations int     `json:"test_iterations"`
	Reads          Runs    `json:"reads"`
	Writes         Writes  `json:"writes"`
	Spawns         int     `json:"spawns"`
	Resumes        int     `json:"resumes"`
}

func (f *Figures) add(o Figures) {
	f.ShareMs += o.ShareMs
	f.BusyMs += o.BusyMs
	f.ModelMs += o.ModelMs
	f.ThinkMs += o.ThinkMs
	f.Requests += o.Requests
	f.Tokens.In += o.Tokens.In
	f.Tokens.Out += o.Tokens.Out
	f.Tokens.CacheRead += o.Tokens.CacheRead
	f.Tokens.CacheWrite += o.Tokens.CacheWrite
	f.Tokens.Thinking += o.Tokens.Thinking
	f.Builds.add(o.Builds)
	f.Tests.add(o.Tests)
	f.Reads.add(o.Reads)
	f.TestIterations += o.TestIterations
	f.Writes.FilesCreated += o.Writes.FilesCreated
	f.Writes.FilesEdited += o.Writes.FilesEdited
	f.Writes.LinesAdded += o.Writes.LinesAdded
	f.Writes.LinesRemoved += o.Writes.LinesRemoved
	f.Spawns += o.Spawns
	f.Resumes += o.Resumes
}

func (r *Runs) add(o Runs) {
	r.Count, r.Ms, r.Failed = r.Count+o.Count, r.Ms+o.Ms, r.Failed+o.Failed
}

type Row struct {
	Key       string `json:"key"`
	Session   string `json:"session,omitempty"`
	Agent     string `json:"agent,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	Figures
}

type Table struct {
	Rows  []Row `json:"rows"`
	Total Row   `json:"total"`
}

type span struct{ from, to int64 }

type Unit struct {
	Session    string
	Agent      string
	AgentType  string
	Ticket     string
	Generation int
	Figures
	spans []span
}

type Book struct{ Units []Unit }

func Session(root, id string, keys []string) (Book, error) {
	leads, err := leadFiles(root, id)
	switch {
	case err != nil:
		return Book{}, err
	case len(leads) == 0:
		return Book{}, fmt.Errorf("no Claude Code session %q in %s", id, root)
	case len(leads) > 1:
		return Book{}, fmt.Errorf("session %q matches %d sessions in %s: give more of the id", id, len(leads), root)
	}
	return read(leads, keys)
}

func Project(root string, keys []string) (Book, error) {
	leads, err := leadFiles(root, "")
	if err != nil {
		return Book{}, err
	}
	return read(leads, keys)
}

func ProjectDir(claudeHome, project string) string {
	return filepath.Join(claudeHome, "projects", regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(project, "-"))
}

func leadFiles(root, prefix string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("no Claude Code transcripts for this project: %w", err)
	}
	return filepath.Glob(filepath.Join(root, prefix+"*"+transcriptExt))
}

func read(leads, keys []string) (Book, error) {
	var paths []string
	bounds := []int{0}
	for _, lead := range leads {
		agents, err := filepath.Glob(filepath.Join(strings.TrimSuffix(lead, transcriptExt), "subagents", agentPrefix+"*"+transcriptExt))
		if err != nil {
			return Book{}, err
		}
		paths = append(append(paths, lead), agents...)
		bounds = append(bounds, len(paths))
	}
	rd := newReader(keys)
	files, err := readAll(paths, rd)
	if err != nil {
		return Book{}, err
	}
	var book Book
	for at, lead := range leads {
		book.Units = append(book.Units, rd.attribute(strings.TrimSuffix(filepath.Base(lead), transcriptExt), files[bounds[at]:bounds[at+1]])...)
	}
	sweep(book.Units)
	return book, nil
}

func (b Book) Fork(n int) Book {
	var kept Book
	for _, unit := range b.Units {
		if unit.Generation == n {
			kept.Units = append(kept.Units, unit)
		}
	}
	return kept
}

func (b Book) BySession() Table {
	return table(b.Units, func(u Unit) Row {
		switch {
		case u.Ticket != "":
			return Row{Key: u.Ticket}
		case u.Agent == "":
			return Row{Key: LeadRow}
		}
		return Row{Key: UnassignedRow}
	})
}

func (b Book) ForTicket(ticket string) Table {
	var mine []Unit
	for _, unit := range b.Units {
		if unit.Ticket == ticket {
			mine = append(mine, unit)
		}
	}
	return table(mine, func(u Unit) Row {
		return Row{Key: ticket, Session: u.Session, Agent: u.Agent, AgentType: u.AgentType}
	})
}

func table(units []Unit, rowOf func(Unit) Row) Table {
	type group struct {
		row   Row
		spans []span
		first int64
	}
	var groups []*group
	byRow := map[Row]*group{}
	var all []span
	total := Row{Key: TotalRow}
	for _, unit := range units {
		key := rowOf(unit)
		g := byRow[key]
		if g == nil {
			g = &group{row: key, first: math.MaxInt64}
			byRow[key] = g
			groups = append(groups, g)
		}
		g.row.add(unit.Figures)
		g.spans = append(g.spans, unit.spans...)
		for _, s := range unit.spans {
			g.first = min(g.first, s.from)
		}
		total.add(unit.Figures)
		all = append(all, unit.spans...)
	}
	rank := func(g *group) int { return slices.Index([]string{LeadRow, UnassignedRow}, g.row.Key) }
	slices.SortFunc(groups, func(a, b *group) int {
		x, y := a.row, b.row
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.first, b.first),
			strings.Compare(x.Key, y.Key), strings.Compare(x.Session, y.Session), strings.Compare(x.Agent, y.Agent))
	})
	rows := make([]Row, len(groups))
	for i, g := range groups {
		g.row.WallMs = union(g.spans)
		rows[i] = g.row
	}
	total.WallMs = union(all)
	return Table{Rows: rows, Total: total}
}

func union(spans []span) int64 {
	sorted := slices.Clone(spans)
	slices.SortFunc(sorted, func(a, b span) int { return cmp.Compare(a.from, b.from) })
	var wall, reach int64 = 0, math.MinInt64
	for _, s := range sorted {
		from := max(s.from, reach)
		if s.to > from {
			wall += s.to - from
		}
		reach = max(reach, s.to)
	}
	return wall
}

func sweep(units []Unit) {
	change := map[int64]int{}
	for _, unit := range units {
		for _, s := range unit.spans {
			change[s.from]++
			change[s.to]--
		}
	}
	times := make([]int64, 0, len(change))
	for at := range change {
		times = append(times, at)
	}
	slices.Sort(times)
	integral := make([]float64, len(times))
	active := 0
	for i := 1; i < len(times); i++ {
		active += change[times[i-1]]
		integral[i] = integral[i-1]
		if active > 0 {
			integral[i] += float64(times[i]-times[i-1]) / float64(active)
		}
	}
	at := func(t int64) float64 {
		i, _ := slices.BinarySearch(times, t)
		return integral[i]
	}
	for i := range units {
		for _, s := range units[i].spans {
			units[i].ShareMs += at(s.to) - at(s.from)
		}
	}
}
