package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const relinkCommand = "tofu migrate --relink"

type relinkReport struct {
	Path     string   `json:"path"`
	Was      []string `json:"was"`
	State    string   `json:"state"`
	Sessions int      `json:"sessions"`
}

func openedProject(args []string) (string, bool) {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		return "", false
	}
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	opens := verb == "" || verb == "--continue" || verb == "run" || verb == "serve" && slices.Contains(args, "--stdio")
	if at := slices.Index(args, "--dir"); opens && at >= 0 && at+1 < len(args) {
		return args[at+1], true
	}
	return ".", opens
}

func openProject(out io.Writer, in io.Reader, dir string, mayAsk bool) {
	if isDir, _ := sys.IsDir(dir); !isDir {
		return
	}
	page := cli.Detect(out, os.Environ())
	project, err := sys.OpenProject(dir)
	if err != nil {
		_ = page.Print(out, page.ErrorLine("the project folder of "+page.Path(dir)+" was not opened: "+err.Error(), ""))
		return
	}
	if project.Registered || project.Moved != nil && offerRelink(page, out, in, project, mayAsk) {
		return
	}
	var copied string
	if project.Legacy != "" {
		files, size, err := copyTreeInto(os.DirFS(project.Legacy), ".", project.State)
		if err != nil {
			_ = page.Print(out, page.ErrorLine(filepath.Base(project.Legacy)+" not copied to "+page.Path(project.State)+", still read from there: "+err.Error(), ""))
			return
		}
		copied = strings.Join([]string{filepath.Base(project.Legacy) + " copied to " + filepath.Base(project.State), plural(countSessions(project.State), "session"),
			plural(files, "file"), widget.Size(int(size)), "old folder kept"}, " · ")
	}
	if err := project.Register(); err != nil {
		_ = page.Print(out, page.ErrorLine("the project folder of "+page.Path(project.Path)+" was not registered: "+err.Error(), ""))
		return
	}
	if copied != "" {
		_ = page.Print(out, []string{page.Receipt(cli.Added, copied, project.State)})
	}
}

func offerRelink(page cli.Page, out io.Writer, in io.Reader, project sys.Project, mayAsk bool) (answered bool) {
	offer := page.Glyph(cli.Warn) + " " + page.Path(project.Path) + " looks like " + page.Path(strings.Join(project.Moved.Paths, ", ")) + ", moved here · " +
		plural(countSessions(filepath.Join(filepath.Dir(project.State), project.Moved.Folder)), "session")
	if !mayAsk {
		_ = page.Print(out, []string{offer + cli.Gap + page.Hint(relinkCommand)})
		return true
	}
	_ = page.Print(out, []string{offer, "relink and keep them? [Y/n] "})
	answer, _ := bufio.NewReader(in).ReadString('\n')
	if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "" && answer != "y" && answer != "yes" {
		return false
	}
	report, err := relink(project)
	if err != nil {
		_ = page.Print(out, page.ErrorLine("not relinked: "+err.Error(), relinkCommand))
		return true
	}
	_ = page.Print(out, []string{relinkLine(page, report)})
	return true
}

func relink(project sys.Project) (relinkReport, error) {
	state, err := project.Relink()
	if err != nil {
		return relinkReport{}, err
	}
	return relinkReport{Path: project.Path, Was: project.Moved.Paths, State: state, Sessions: countSessions(state)}, nil
}

func relinkLine(page cli.Page, report relinkReport) string {
	return page.Receipt(cli.Changed, "relinked from "+page.Path(strings.Join(report.Was, ", "))+" · "+plural(report.Sessions, "session")+" kept", report.State)
}

func relinkVerb(o verbOutput) int {
	dir, err := os.Getwd()
	var project sys.Project
	if err == nil {
		project, err = sys.OpenProject(dir)
	}
	var report relinkReport
	if err == nil {
		report, err = relink(project)
	}
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, report, func(page cli.Page) []string { return []string{relinkLine(page, report)} })
}
