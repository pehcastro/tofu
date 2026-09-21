package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
)

func settingsPaths(dir string) (global, project string) {
	home, _ := sys.HomeConfigDir()
	return filepath.Join(home, settingspkg.FileName), filepath.Join(sys.StateDir(dir), settingspkg.FileName)
}

func openSettings(dir string) (*settingspkg.Store, error) {
	global, project := settingsPaths(dir)
	return settingspkg.Open(global, project)
}

func appDecisionCap(dir string) int {
	store, err := openSettings(dir)
	if err != nil {
		return 0
	}
	return store.Int(settingspkg.DecisionCap)
}

func settingsVerb(args []string, out, errOut io.Writer) int {
	dir, err := os.Getwd()
	if err != nil {
		return settingsFail(errOut, err)
	}
	store, err := openSettings(dir)
	if err != nil {
		return settingsFail(errOut, err)
	}
	if len(args) == 0 {
		printSettingsList(out, store)
		return exitOK
	}
	switch args[0] {
	case "get":
		return settingsGetVerb(args[1:], store, out, errOut)
	case "set":
		return settingsSetVerb(args[1:], store, out, errOut)
	default:
		return settingsFail(errOut, fmt.Errorf("unknown subcommand %q, want get or set", args[0]))
	}
}

func settingsFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu settings: %v\n", err)
	return exitUsage
}

func printSettingsList(out io.Writer, store *settingspkg.Store) {
	widest := 0
	for _, spec := range store.Table() {
		widest = max(widest, len(spec.Key))
	}
	for _, spec := range store.Table() {
		scope, fromFile := store.Source(spec.Key)
		source := "default"
		if fromFile {
			source = scope.String() + " " + store.Path(scope)
		}
		_, _ = fmt.Fprintf(out, "%-*s %-6s %s\n", widest, spec.Key, settingsDisplay(store, spec), source)
	}
}

func settingsDisplay(store *settingspkg.Store, spec settingspkg.Spec) string {
	if spec.Kind == settingspkg.Bool {
		return strconv.FormatBool(store.Bool(spec.Key))
	}
	return strconv.Itoa(store.Int(spec.Key))
}

func settingsGetVerb(args []string, store *settingspkg.Store, out, errOut io.Writer) int {
	if len(args) != 1 {
		return settingsFail(errOut, errors.New("usage: tofu settings get <key>"))
	}
	spec, known := specByKey(store, args[0])
	if !known {
		return settingsFail(errOut, fmt.Errorf("%q is not a declared setting", args[0]))
	}
	_, _ = fmt.Fprintln(out, settingsDisplay(store, spec))
	return exitOK
}

func settingsSetVerb(args []string, store *settingspkg.Store, out, errOut io.Writer) int {
	scope := settingspkg.Global
	rest := args
	if len(args) >= 2 && args[0] == "--scope" {
		switch args[1] {
		case "global":
			scope = settingspkg.Global
		case "project":
			scope = settingspkg.Project
		default:
			return settingsFail(errOut, fmt.Errorf("--scope wants global or project, got %q", args[1]))
		}
		rest = args[2:]
	}
	if len(rest) != 2 {
		return settingsFail(errOut, errors.New("usage: tofu settings set [--scope global|project] <key> <value>"))
	}
	spec, known := specByKey(store, rest[0])
	if !known {
		return settingsFail(errOut, fmt.Errorf("%q is not a declared setting", rest[0]))
	}
	value, err := parseSettingValue(spec, rest[1])
	if err != nil {
		return settingsFail(errOut, err)
	}
	if err := store.Set(scope, spec.Key, value); err != nil {
		return settingsFail(errOut, err)
	}
	_, _ = fmt.Fprintf(out, "%s set to %s in the %s file\n", spec.Key, rest[1], scope)
	return exitOK
}

func specByKey(store *settingspkg.Store, key string) (settingspkg.Spec, bool) {
	for _, spec := range store.Table() {
		if spec.Key == key {
			return spec, true
		}
	}
	return settingspkg.Spec{}, false
}

func parseSettingValue(spec settingspkg.Spec, raw string) (int, error) {
	if spec.Kind == settingspkg.Bool {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return 0, fmt.Errorf("%s takes true or false, got %q", spec.Key, raw)
		}
		if parsed {
			return 1, nil
		}
		return 0, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s takes a number, got %q", spec.Key, raw)
	}
	return parsed, nil
}
