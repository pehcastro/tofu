package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/platform/fs"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
)

const projectsUsage = "tofu projects [--json]"

type projectsReport struct {
	Projects []projectListed `json:"projects"`
}

type projectListed struct {
	ID       string           `json:"id"`
	Path     string           `json:"path"`
	Name     string           `json:"name"`
	LastUsed *time.Time       `json:"lastUsed"`
	Missing  bool             `json:"missing,omitempty"`
	Host     *projectHost     `json:"host"`
	Sessions []projectSession `json:"sessions"`
	Skipped  int              `json:"skipped"`
	used     time.Time
}

type projectHost struct {
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

type projectSession struct {
	ID        string            `json:"id"`
	Name      string            `json:"name,omitempty"`
	Handle    string            `json:"handle"`
	LastAt    time.Time         `json:"lastAt"`
	Working   bool              `json:"working"`
	EndedAt   *time.Time        `json:"endedAt,omitempty"`
	EndReason session.EndReason `json:"endReason,omitempty"`
}

func projectsVerb(args []string, out, errOut io.Writer) int {
	operands, asJSON, err := output.VerbArgs(args)
	o := output.Verb{Name: "projects", UsageLine: projectsUsage, AsJSON: asJSON, Out: out, ErrOut: errOut}
	if err == nil && len(operands) > 0 {
		err = errors.New("tofu projects takes no operand")
	}
	if err != nil {
		return o.Usage(err)
	}
	home, err := fs.HomeConfigDir()
	var entries []platformproject.ProjectEntry
	if err == nil {
		entries, err = platformproject.Registered(home)
	}
	var hubs []host.HubStatus
	if err == nil {
		hubs, err = host.RunningHubs()
	}
	if err != nil {
		return o.Fail(err)
	}
	states, _ := os.ReadDir(filepath.Join(home, fs.ProjectsDirName))
	report, next := projectsReport{Projects: make([]projectListed, len(entries))}, make(chan int)
	var listers sync.WaitGroup
	for range min(max(goruntime.GOMAXPROCS(0)/konst.CPUsPerReadWorker, 1), len(entries)) {
		listers.Go(func() {
			for i := range next {
				_, kept := slices.BinarySearchFunc(states, entries[i].Folder, func(state os.DirEntry, folder string) int { return strings.Compare(state.Name(), folder) })
				report.Projects[i] = listedProject(home, entries[i], kept, hubs)
			}
		})
	}
	for i := range entries {
		next <- i
	}
	close(next)
	listers.Wait()
	slices.SortStableFunc(report.Projects, func(a, b projectListed) int { return b.used.Compare(a.used) })
	now := time.Now()
	return o.Done(true, report, func(page cli.Page) []string { return projectsLines(page, report, now) })
}

func listedProject(home string, entry platformproject.ProjectEntry, hasState bool, hubs []host.HubStatus) projectListed {
	listed := projectListed{ID: entry.Folder, Missing: true, Sessions: []projectSession{}, used: entry.Used}
	if at := slices.IndexFunc(entry.Paths, func(path string) bool { found, _ := fs.Exists(path); return found }); at >= 0 {
		listed.Path, listed.Missing = entry.Paths[at], false
	} else if len(entry.Paths) > 0 {
		listed.Path = entry.Paths[0]
	}
	if at := slices.IndexFunc(hubs, func(hub host.HubStatus) bool { return platformproject.SamePath(hub.Project, listed.Path) }); at >= 0 {
		listed.Host = &projectHost{PID: hubs[at].PID, Version: hubs[at].Version}
	}
	listed.Name = filepath.Base(listed.Path)
	if !entry.Used.IsZero() {
		listed.LastUsed = &entry.Used
	}
	if !hasState {
		return listed
	}
	outlined, skipped, err := session.OpenAt(filepath.Join(home, fs.ProjectsDirName, entry.Folder)).Outline()
	if err != nil {
		return listed
	}
	listed.Skipped = len(skipped)
	for _, family := range outlined {
		if slices.ContainsFunc(family.Generations, func(header session.Header) bool { return header.Kind == session.KindSide }) {
			continue
		}
		head := family.Head()
		listed.Sessions = append(listed.Sessions, projectSession{ID: head.ID, Name: head.Named(), Handle: family.Handle(), LastAt: family.Touched, Working: family.Working, EndedAt: head.EndedAt, EndReason: head.EndReason})
	}
	return listed
}

func projectsLines(page cli.Page, report projectsReport, now time.Time) []string {
	if len(report.Projects) == 0 {
		return page.Title("Projects", nil, cli.Verdict{Mark: cli.Idle, Text: "none recorded"})
	}
	rows := make([]cli.Row, len(report.Projects))
	for i, listed := range report.Projects {
		used := "never opened"
		if listed.LastUsed != nil {
			used = sessionWhen(*listed.LastUsed, now)
		}
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{listed.Name, used, output.Plural(len(listed.Sessions), "session")}, Detail: listed.Path}
		switch {
		case listed.Missing:
			rows[i].Mark, rows[i].Detail = cli.Warn, listed.Path+" is gone"
		case slices.ContainsFunc(listed.Sessions, func(row projectSession) bool { return row.Working }):
			rows[i].Mark = cli.Active
		}
	}
	return append(append(page.Title("Projects", []string{output.Plural(len(report.Projects), "project")}, cli.Verdict{}), ""), cli.Indent(page.Rows(rows)...)...)
}
