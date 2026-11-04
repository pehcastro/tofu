package browser

import (
	"fmt"
	"os"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/internal/tools/web/browser/driver"
)

const ProfileUsage = "tofu browser profile [list | new <name> | login <name> [<url>] | stop <name> | remove <name>] [--json]"

func (o browserOutput) profile(args []string) int {
	verb, name := "list", ""
	if len(args) > 0 {
		verb = args[0]
	}
	if len(args) > 1 {
		name = args[1]
	}
	arity, known := map[string][2]int{"list": {0, 1}, "new": {2, 2}, "login": {2, 3}, "stop": {2, 2}, "remove": {2, 2}}[verb]
	if !known {
		_, _ = fmt.Fprintf(o.errOut, "tofu browser profile: unknown argument %q: use list, new, login, stop or remove\n", verb)
		return output.ExitUsage
	}
	if len(args) < arity[0] || len(args) > arity[1] {
		_, _ = fmt.Fprintln(o.errOut, "usage: "+ProfileUsage)
		return output.ExitUsage
	}
	o.verb = "browser profile " + verb
	home, err := os.UserHomeDir()
	if err != nil {
		return o.fail(err)
	}
	type done struct {
		Profile string `json:"profile"`
		Dir     string `json:"dir,omitempty"`
		Running bool   `json:"was_running,omitempty"`
		URL     string `json:"url,omitempty"`
	}
	switch verb {
	case "list":
		profiles, err := driver.Profiles(home)
		if err != nil {
			return o.fail(err)
		}
		rows := make([]page.Row, len(profiles))
		for i, profile := range profiles {
			rows[i] = page.Row{Mark: page.Idle, Cells: []string{profile.Name, "stopped", o.page.Path(profile.Dir)}}
			if profile.Running {
				rows[i].Mark, rows[i].Cells[1] = page.Active, "running"
			}
		}
		lines := append(o.page.Title("Browser profiles", []string{fmt.Sprint(len(profiles)) + " profiles", o.page.Path(driver.ProfilesDir(home))}, page.Verdict{Mark: page.Done, Text: "listed"}), "")
		if len(profiles) == 0 {
			lines = append(lines, page.Indent(o.page.Label("tofu browser profile new <name> makes one; browserProfile picks it"))...)
		}
		return o.show(struct {
			Folder   string           `json:"folder"`
			Profiles []driver.Profile `json:"profiles"`
		}{driver.ProfilesDir(home), append([]driver.Profile{}, profiles...)}, append(lines, page.Indent(o.page.Rows(rows)...)...))
	case "new":
		profile, err := driver.NewProfile(home, name)
		if err != nil {
			return o.fail(err)
		}
		return o.show(done{Profile: name, Dir: profile.Dir}, []string{o.page.Receipt(page.Added, "made browser profile "+name, o.page.Path(profile.Dir))})
	case "login":
		url := driver.LoginURL
		if len(args) == 3 {
			url = args[2]
		}
		if err := driver.LoginProfile(home, name, url); err != nil {
			return o.fail(err)
		}
		lines := []string{o.page.Receipt(page.Added, "opened Chrome on browser profile "+name, url), ""}
		return o.show(done{Profile: name, URL: url}, append(lines, page.Indent(o.page.Hint("sign in, then close that window: an agent cannot start this profile while it is open"))...))
	case "stop":
		running, err := driver.StopProfile(home, name)
		if err != nil {
			return o.fail(err)
		}
		said := "browser profile " + name + " was not running"
		if running {
			said = "stopped the Chrome on browser profile " + name
		}
		return o.show(done{Profile: name, Running: running}, []string{o.page.Glyph(page.Removed) + " " + said})
	}
	if err := driver.RemoveProfile(home, name); err != nil {
		return o.fail(err)
	}
	return o.show(done{Profile: name}, []string{o.page.Glyph(page.Removed) + " removed browser profile " + name})
}
