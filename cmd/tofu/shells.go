package main

import (
	"fmt"
	"io"

	settingspkg "tofu/internal/settings"
	"tofu/internal/shell"
	"tofu/internal/sys"
)

const shellsUsage = "usage: tofu shells list | tofu shells log <name> | tofu shells kill <name>"

func shellsVerb(args []string, out, errOut io.Writer) int {
	registry, err := openShellRegistry()
	if err != nil {
		return shellsFail(errOut, err)
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(errOut, shellsUsage)
		return exitUsage
	}
	switch args[0] {
	case "list":
		return shellsList(registry, out, errOut)
	case "log":
		return shellsLog(registry, args[1:], out, errOut)
	case "kill":
		return shellsKill(registry, args[1:], out, errOut)
	default:
		_, _ = fmt.Fprintf(errOut, "tofu shells: unknown verb %q\n\n%s\n", args[0], shellsUsage)
		return exitUsage
	}
}

func openShellRegistry() (*shell.Registry, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return nil, err
	}
	return shell.OpenAt(sys.Join(state, "shells")), nil
}

func launchShellRegistry(dir string) (*shell.Registry, error) {
	registry, err := openShellRegistry()
	if err == nil && settingInt(dir, settingspkg.PersistentRegistry, nil) != 0 {
		registry.Lifetime = shell.OutlivesTofu
	}
	return registry, err
}

func shellsList(registry *shell.Registry, out, errOut io.Writer) int {
	shells, err := registry.List()
	if err != nil {
		return shellsFail(errOut, err)
	}
	if len(shells) == 0 {
		_, _ = fmt.Fprintln(out, "no process is registered")
		return exitOK
	}
	for _, one := range shells {
		_, _ = fmt.Fprintf(out, "%-16s %-8s pid %-7d owner %-12s dir %s  %s\n", one.Name, one.State, one.PID, ownerName(one.Owner), one.Dir, one.Command)
	}
	return exitOK
}

func shellsLog(registry *shell.Registry, args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(errOut, shellsUsage)
		return exitUsage
	}
	log, err := registry.Tail(args[0], shell.DefaultTail)
	if err != nil {
		return shellsFail(errOut, err)
	}
	_, _ = fmt.Fprintln(out, log)
	return exitOK
}

func shellsKill(registry *shell.Registry, args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(errOut, shellsUsage)
		return exitUsage
	}
	if err := registry.Kill(args[0]); err != nil {
		return shellsFail(errOut, err)
	}
	_, _ = fmt.Fprintln(out, args[0]+" killed")
	return exitOK
}

func shellsFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu shells: %v\n", err)
	return exitUsage
}
