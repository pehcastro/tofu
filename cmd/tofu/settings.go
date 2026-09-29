package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	settingsUsage    = "tofu settings [get <key> | set [--scope global|project] <key> <value>] [--json]"
	settingsGetUsage = "tofu settings get <key> [--json]"
	settingsSetUsage = "tofu settings set [--scope global|project] <key> <value> [--json]"
)

type settingValue struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Value    any    `json:"value"`
	Source   string `json:"source"`
}

type settingsReport struct {
	Global   string         `json:"global_file"`
	Project  string         `json:"project_file"`
	Settings []settingValue `json:"settings"`
}

func settingsPaths(dir string) (global, project string) {
	home, _ := sys.HomeConfigDir()
	return filepath.Join(home, settingspkg.FileName), filepath.Join(sys.StateDir(dir), settingspkg.FileName)
}

func openSettings(dir string) (*settingspkg.Store, error) {
	global, project := settingsPaths(dir)
	return settingspkg.Open(global, project)
}

func appSetting(dir, key string) (value int, unreadable string) {
	store, err := openSettings(dir)
	if err != nil {
		fallback := settingspkg.DeclaredDefault(key)
		return fallback, fmt.Sprintf("%s fell back to its default of %d because the settings file could not be read: %v", key, fallback, err)
	}
	return store.Int(key), ""
}

func appTextSetting(dir, key string) (value string, unreadable string) {
	store, err := openSettings(dir)
	if err != nil {
		fallback := settingspkg.DeclaredDefaultText(key)
		return fallback, fmt.Sprintf("%s fell back to its default of %q because the settings file could not be read: %v", key, fallback, err)
	}
	return store.Text(key), ""
}

func settingInt(dir, key string, say func(string)) int {
	value, unreadable := appSetting(dir, key)
	if unreadable != "" && say != nil {
		say(unreadable)
	}
	return value
}

func settingText(dir, key string, say func(string)) string {
	value, unreadable := appTextSetting(dir, key)
	if unreadable != "" && say != nil {
		say(unreadable)
	}
	return value
}

func settingsVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "settings", usageLine: settingsUsage, asJSON: slices.Contains(args, jsonFlag), out: out, errOut: errOut}
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag })
	dir, err := os.Getwd()
	var store *settingspkg.Store
	if err == nil {
		store, err = openSettings(dir)
	}
	if err != nil {
		return o.fail(err)
	}
	if len(args) == 0 {
		report := settingsReport{Global: store.Path(settingspkg.Global), Project: store.Path(settingspkg.Project)}
		for _, spec := range store.Table() {
			report.Settings = append(report.Settings, resolvedSetting(store, spec))
		}
		return show(out, o.asJSON, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: report},
			func(page cli.Page) []string { return settingsPage(page, report) })
	}
	switch args[0] {
	case "get":
		o.verb, o.usageLine = "settings get", settingsGetUsage
		return settingsGetVerb(o, args[1:], store)
	case "set":
		o.verb, o.usageLine = "settings set", settingsSetUsage
		return settingsSetVerb(o, args[1:], store)
	}
	return o.usage(fmt.Errorf("unknown subcommand %q", args[0]))
}

func printSettingsList(out io.Writer, store *settingspkg.Store) {
	widest, category := 0, 0
	for _, spec := range store.Table() {
		widest, category = max(widest, len(spec.Key)), max(category, len(spec.Category))
	}
	for _, spec := range store.Table() {
		scope, fromFile := store.Source(spec.Key)
		source := "default"
		if fromFile {
			source = scope.String() + " " + store.Path(scope)
		}
		_, _ = fmt.Fprintf(out, "%-*s %-*s %-6v %s\n", category, spec.Category, widest, spec.Key, settingOf(store, spec), source)
	}
}

func settingOf(store *settingspkg.Store, spec settingspkg.Spec) any {
	switch spec.Kind {
	case settingspkg.Bool:
		return store.Bool(spec.Key)
	case settingspkg.Text:
		return store.Text(spec.Key)
	default:
		return store.Int(spec.Key)
	}
}

func resolvedSetting(store *settingspkg.Store, spec settingspkg.Spec) settingValue {
	source := "default"
	if scope, fromFile := store.Source(spec.Key); fromFile {
		source = scope.String()
	}
	return settingValue{Key: spec.Key, Category: spec.Category, Value: settingOf(store, spec), Source: source}
}

func settingsPage(page cli.Page, report settingsReport) []string {
	rows := make([]cli.Row, len(report.Settings))
	set := 0
	for i, setting := range report.Settings {
		rows[i] = cli.Row{Cells: []string{setting.Key, cmp.Or(fmt.Sprint(setting.Value), "empty"), setting.Source}}
		if setting.Source != "default" {
			rows[i].Mark = cli.Active
			set++
		}
	}
	lines := page.Title("Settings", []string{strconv.Itoa(set) + " set"}, cli.Verdict{Text: strconv.Itoa(len(rows)) + " declared"})
	category := ""
	for i, line := range page.Rows(rows) {
		if report.Settings[i].Category != category {
			category = report.Settings[i].Category
			lines = append(lines, "", page.Section(category, cli.Verdict{}))
		}
		lines = append(lines, cli.Indent(line)...)
	}
	lines = append(lines, "", page.Section("stored in", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "global", Text: page.Path(report.Global)}, {Label: "project", Text: page.Path(report.Project)}})...)...)
	return append(append(lines, ""), cli.Indent(page.Hint("tofu docs settings"))...)
}

func settingsGetVerb(o verbOutput, args []string, store *settingspkg.Store) int {
	if len(args) != 1 {
		return o.usage(errors.New("wants one key"))
	}
	spec, known := specByKey(store, args[0])
	if !known {
		return o.usage(fmt.Errorf("%q is not a declared setting", args[0]))
	}
	setting := resolvedSetting(store, spec)
	return show(o.out, o.asJSON, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: setting},
		func(cli.Page) []string { return []string{fmt.Sprint(setting.Value)} })
}

func settingsSetVerb(o verbOutput, args []string, store *settingspkg.Store) int {
	scope := settingspkg.Global
	rest := args
	if len(args) >= 2 && args[0] == "--scope" {
		switch args[1] {
		case "global":
		case "project":
			scope = settingspkg.Project
		default:
			return o.usage(fmt.Errorf("--scope wants global or project, got %q", args[1]))
		}
		rest = args[2:]
	}
	if len(rest) != 2 {
		return o.usage(errors.New("wants a key and a value"))
	}
	spec, known := specByKey(store, rest[0])
	if !known {
		return o.usage(fmt.Errorf("%q is not a declared setting", rest[0]))
	}
	before := fmt.Sprint(settingOf(store, spec))
	if before == "" || strings.ContainsAny(before, " \t") {
		before = strconv.Quote(before)
	}
	if err := writeSetting(store, scope, spec, rest[1]); err != nil {
		return o.usage(err)
	}
	return o.receipt(writeReceipt{
		Changes: []fileChange{{Change: changeChanged, What: spec.Key + " = " + rest[1], File: store.Path(scope)}},
		Undo:    "tofu settings set --scope " + scope.String() + " " + spec.Key + " " + before,
	})
}

func writeSetting(store *settingspkg.Store, scope settingspkg.Scope, spec settingspkg.Spec, raw string) error {
	if spec.Kind == settingspkg.Text {
		return store.SetText(scope, spec.Key, raw)
	}
	value, err := parseSettingValue(spec, raw)
	if err != nil {
		return err
	}
	return store.Set(scope, spec.Key, value)
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
