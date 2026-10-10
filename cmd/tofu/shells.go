package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	settingspkg "tofu/internal/settings"
	"tofu/internal/shell"
	"tofu/internal/sys"
)

const shellsSubcommands = "tofu shells list|log|stop|restart <name> [--json]"

func shellsVerb(args []string, out, errOut io.Writer) int {
	names, _, err := verbArgs(args[min(1, len(args)):])
	o := verbOutput{verb: "shells", usageLine: shellsSubcommands, asJSON: jsonAsked(args), out: out, errOut: errOut}
	if len(withoutJSON(args)) == 0 {
		return o.usage(errors.New("no subcommand"))
	}
	wanted := 1
	switch args[0] {
	case "list":
		wanted = 0
	case "log", "stop", "kill", "restart":
	default:
		return o.usage(fmt.Errorf("there is no subcommand %q", args[0]))
	}
	o.verb = "shells " + args[0]
	if err == nil && len(names) != wanted {
		err = fmt.Errorf("%d names, want %d", len(names), wanted)
	}
	if err != nil {
		return o.usage(err)
	}
	registry, err := launchShellRegistry(".")
	if err != nil {
		return o.fail(err)
	}
	switch args[0] {
	case "list":
		return shellsList(o, registry)
	case "log":
		return shellsLog(o, registry, names[0])
	case "restart":
		return shellsRestart(o, registry, names[0])
	}
	return shellsStop(o, registry, names[0])
}

func launchShellRegistry(dir string) (*shell.Registry, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return nil, err
	}
	registry := shell.OpenAt(sys.Join(state, "shells"))
	if settingInt(dir, settingspkg.PersistentRegistry, nil) != 0 {
		registry.Lifetime = shell.OutlivesTofu
	}
	return registry, nil
}

type shellReading struct {
	shell.Shell
	ElapsedMS  int64      `json:"elapsed_ms"`
	LastOutput *time.Time `json:"last_output,omitempty"`
	timing     shell.Timing
}

func readShell(registry *shell.Registry, one shell.Shell, now time.Time) shellReading {
	timing := registry.Timing(one, now)
	reading := shellReading{Shell: one, ElapsedMS: timing.Ran.Milliseconds(), timing: timing}
	if !timing.Last.IsZero() {
		reading.LastOutput = &timing.Last
	}
	return reading
}

func shellsList(o verbOutput, registry *shell.Registry) int {
	shells, err := registry.List()
	if err != nil {
		return o.fail(err)
	}
	shells = slices.DeleteFunc(shells, shell.Shell.OneShot)
	now := time.Now()
	readings := make([]shellReading, len(shells))
	for i, one := range shells {
		readings[i] = readShell(registry, one, now)
	}
	return o.done(true, struct {
		Shells []shellReading `json:"shells"`
	}{readings}, func(page cli.Page) []string { return shellsLines(page, readings, now) })
}

func shellsLines(page cli.Page, shells []shellReading, now time.Time) []string {
	if len(shells) == 0 {
		return page.Title("Shells", nil, cli.Verdict{Mark: cli.Idle, Text: "none registered"})
	}
	running := 0
	rows := make([]cli.Row, len(shells))
	for i, one := range shells {
		mark, state := cli.Idle, string(one.State)
		switch {
		case one.LeftOver():
			mark, state = cli.Warn, "left over"
		case one.State == shell.Running:
			mark = cli.Active
		case one.ExitCode != nil:
			state += " " + strconv.Itoa(*one.ExitCode)
		}
		if one.State == shell.Running {
			running++
		}
		rows[i] = cli.Row{Mark: mark, Cells: []string{one.Name, state, "pid " + strconv.Itoa(one.PID), ownerName(one.Owner)}, Detail: one.Command + " · " + page.Path(one.Dir) + " · " + one.timing.Words(now)}
	}
	facts := []string{plural(len(shells), "shell")}
	if running > 0 {
		facts = append(facts, strconv.Itoa(running)+" running")
	}
	return append(append(page.Title("Shells", facts, cli.Verdict{}), ""), cli.Indent(page.Rows(rows)...)...)
}

func shellsLog(o verbOutput, registry *shell.Registry, name string) int {
	entry, err := registry.Read(name)
	if err != nil {
		return shellsFailed(o, name, err)
	}
	log, err := registry.Tail(name, shell.DefaultTail)
	if err != nil {
		return shellsFailed(o, name, err)
	}
	lines := []string{}
	if log != "" {
		lines = strings.Split(log, "\n")
	}
	now := time.Now()
	reading := readShell(registry, entry, now)
	return o.done(true, struct {
		shellReading
		Lines []string `json:"lines"`
	}{reading, lines}, func(page cli.Page) []string {
		printed := append(page.Title("Log", []string{name, string(entry.State), reading.timing.Words(now), plural(len(lines), "line")}, cli.Verdict{}), "")
		if len(lines) == 0 {
			return append(printed, cli.Indent(page.Label("empty"))...)
		}
		return append(printed, cli.Indent(lines...)...)
	})
}

func shellsStop(o verbOutput, registry *shell.Registry, name string) int {
	if err := registry.Kill(name); err != nil {
		return shellsFailed(o, name, err)
	}
	return o.done(true, struct {
		Name string `json:"name"`
	}{name}, func(page cli.Page) []string {
		return []string{page.Glyph(cli.Removed) + " stopped " + name + cli.Gap + page.Label("with every process it started")}
	})
}

func shellsRestart(o verbOutput, registry *shell.Registry, name string) int {
	started, err := registry.Restart(name)
	if err != nil {
		return shellsFailed(o, name, err)
	}
	return o.done(true, started, func(page cli.Page) []string {
		lines := []string{page.Glyph(cli.Added) + " restarted " + name + cli.Gap + page.Label("pid "+strconv.Itoa(started.PID))}
		return append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "dir", Text: page.Path(started.Dir)}, {Label: "command", Text: started.Command}})...)...)
	})
}

func shellsFailed(o verbOutput, name string, err error) int {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = problemError{What: "no shell " + name, Hint: "tofu shells list"}
	case errors.Is(err, shell.ErrNotRunning):
		err = problemError{What: name + " is not running", Hint: "tofu shells restart " + name}
	}
	return o.fail(err)
}
