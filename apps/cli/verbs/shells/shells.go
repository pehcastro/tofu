package shells

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/tools/shell/process"
)

const Subcommands = "tofu shells list|log|stop|restart <name> [--json]"

func Verb(args []string, out, errOut io.Writer) int {
	names, _, err := output.VerbArgs(args[min(1, len(args)):])
	o := output.Verb{Name: "shells", UsageLine: Subcommands, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	if len(output.WithoutJSON(args)) == 0 {
		return o.Usage(errors.New("no subcommand"))
	}
	wanted := 1
	switch args[0] {
	case "list":
		wanted = 0
	case "log", "stop", "kill", "restart":
	default:
		return o.Usage(fmt.Errorf("there is no subcommand %q", args[0]))
	}
	o.Name = "shells " + args[0]
	if err == nil && len(names) != wanted {
		err = fmt.Errorf("%d names, want %d", len(names), wanted)
	}
	if err != nil {
		return o.Usage(err)
	}
	registry, err := LaunchShellRegistry(".")
	if err != nil {
		return o.Fail(err)
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

func LaunchShellRegistry(dir string) (*shell.Registry, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return nil, err
	}
	registry := shell.OpenAt(sys.Join(state, "shells"))
	if settingspkg.IntSaying(dir, settingspkg.PersistentRegistry, nil) != 0 {
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

func shellsList(o output.Verb, registry *shell.Registry) int {
	shells, err := registry.List()
	if err != nil {
		return o.Fail(err)
	}
	shells = slices.DeleteFunc(shells, shell.Shell.OneShot)
	now := time.Now()
	readings := make([]shellReading, len(shells))
	for i, one := range shells {
		readings[i] = readShell(registry, one, now)
	}
	return o.Done(true, struct {
		Shells []shellReading `json:"shells"`
	}{readings}, func(page cli.Page) []string { return shellsLines(page, readings, now) })
}

func shellsLines(page cli.Page, shells []shellReading, now time.Time) []string {
	if len(shells) == 0 {
		return page.Title("Shells", nil, cli.Verdict{Mark: cli.Idle, Text: "none running"})
	}
	rows := make([]cli.Row, len(shells))
	for i, one := range shells {
		mark, state := cli.Active, string(one.State)
		switch {
		case one.LeftOver():
			mark, state = cli.Warn, "left over"
		case one.HeldByPerson():
			state = "running, held by a person"
		}
		rows[i] = cli.Row{Mark: mark, Cells: []string{one.Name, state, "pid " + strconv.Itoa(one.PID), wiring.OwnerName(one.Owner)}, Detail: one.Command + " · " + page.Path(one.Dir) + " · " + one.timing.Words(now)}
	}
	return append(append(page.Title("Shells", []string{output.Plural(len(shells), "shell") + " running"}, cli.Verdict{}), ""), cli.Indent(page.Rows(rows)...)...)
}

func shellsLog(o output.Verb, registry *shell.Registry, name string) int {
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
	return o.Done(true, struct {
		shellReading
		Lines []string `json:"lines"`
	}{reading, lines}, func(page cli.Page) []string {
		printed := append(page.Title("Log", []string{name, string(entry.State), reading.timing.Words(now), output.Plural(len(lines), "line")}, cli.Verdict{}), "")
		if len(lines) == 0 {
			return append(printed, cli.Indent(page.Label("empty"))...)
		}
		return append(printed, cli.Indent(lines...)...)
	})
}

func shellsStop(o output.Verb, registry *shell.Registry, name string) int {
	if err := registry.Kill(name); err != nil {
		return shellsFailed(o, name, err)
	}
	return o.Done(true, struct {
		Name string `json:"name"`
	}{name}, func(page cli.Page) []string {
		return []string{page.Glyph(cli.Removed) + " stopped " + name + cli.Gap + page.Label("with every process it started")}
	})
}

func shellsRestart(o output.Verb, registry *shell.Registry, name string) int {
	started, err := registry.Restart(name)
	if err != nil {
		return shellsFailed(o, name, err)
	}
	return o.Done(true, started, func(page cli.Page) []string {
		lines := []string{page.Glyph(cli.Added) + " restarted " + name + cli.Gap + page.Label("pid "+strconv.Itoa(started.PID))}
		return append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "dir", Text: page.Path(started.Dir)}, {Label: "command", Text: started.Command}})...)...)
	})
}

func shellsFailed(o output.Verb, name string, err error) int {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = output.ProblemError{What: "no shell " + name, Hint: "tofu shells list"}
	case errors.Is(err, shell.ErrNotRunning):
		err = output.ProblemError{What: name + " is not running", Hint: "tofu shells restart " + name}
	}
	return o.Fail(err)
}
