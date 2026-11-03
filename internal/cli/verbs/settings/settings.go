package settings

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/cli/output"
	"tofu/internal/cli/page"
	"tofu/internal/cli/wiring"
	"tofu/internal/host"
	"tofu/internal/project/boardy"
	settingspkg "tofu/internal/store/settings"
)

const (
	Usage            = "tofu settings [get <key> | set [--scope global|project] <key> <value>] [--json]"
	settingsGetUsage = "tofu settings get <key> [--json]"
	settingsSetUsage = "tofu settings set [--scope global|project] <key> <value> [--json]"
)

type (
	settingValue   = host.SettingValue
	settingsReport = host.SettingsReport
)

func Verb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "settings", UsageLine: Usage, AsJSON: slices.Contains(args, output.JSONFlag), Out: out, ErrOut: errOut}
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == output.JSONFlag })
	dir, err := os.Getwd()
	var store *settingspkg.Store
	if err == nil {
		store, err = settingspkg.OpenFor(dir)
	}
	if err != nil {
		return o.Fail(err)
	}
	if len(args) == 0 {
		report := settingsReport{Global: store.Path(settingspkg.Global), Project: store.Path(settingspkg.Project)}
		for _, spec := range store.Table() {
			report.Settings = append(report.Settings, resolvedSetting(store, spec, dir))
		}
		return output.Show(out, o.AsJSON, cli.Envelope{Verb: o.Name, OK: true, At: time.Now(), Data: report},
			func(page cli.Page) []string { return settingsPage(page, report) })
	}
	switch args[0] {
	case "get":
		o.Name, o.UsageLine = "settings get", settingsGetUsage
		return settingsGetVerb(o, args[1:], store, dir)
	case "set":
		o.Name, o.UsageLine = "settings set", settingsSetUsage
		return settingsSetVerb(o, args[1:], store, dir)
	}
	return o.Usage(fmt.Errorf("unknown subcommand %q", args[0]))
}

func PrintList(out io.Writer, store *settingspkg.Store, dir string) {
	widest, category := 0, 0
	for _, spec := range store.Table() {
		widest, category = max(widest, len(spec.Key)), max(category, len(spec.Category))
	}
	for _, spec := range store.Table() {
		setting := resolvedSetting(store, spec, dir)
		source := setting.Source
		if scope, fromFile := store.Source(spec.Key); fromFile {
			source += " " + store.Path(scope)
		}
		_, _ = fmt.Fprintf(out, "%-*s %-*s %-6v %s\n", category, spec.Category, widest, spec.Key, setting.Value, joinedWhy(source, setting.Reason))
	}
}

func joinedWhy(source, because string) string {
	if because == "" {
		return source
	}
	return source + ", " + because
}

func managementInForce(store *settingspkg.Store, dir string) (string, string) {
	_, set := store.Source(boardy.ManagementSetting)
	switch {
	case !wiring.ProjectHasBoard(dir):
		return string(boardy.ManagementOff), "off until the project has a board: tofu boardy init"
	case wiring.BoardManaged(dir) && !set:
		return string(boardy.ManagementBoardy), "on because the project has a board"
	}
	return store.Text(boardy.ManagementSetting), ""
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

func resolvedSetting(store *settingspkg.Store, spec settingspkg.Spec, dir string) settingValue {
	source := "default"
	if scope, fromFile := store.Source(spec.Key); fromFile {
		source = scope.String()
	}
	value, because := settingOf(store, spec), ""
	if spec.Key == boardy.ManagementSetting {
		value, because = managementInForce(store, dir)
	}
	row := settingValue{Key: spec.Key, Category: spec.Category, Value: value, Source: source,
		Label: spec.Label, Description: spec.Description, Kind: host.SettingText, Unit: spec.Unit, Restart: spec.Restart, Reason: because}
	switch {
	case spec.Kind == settingspkg.Bool:
		row.Kind = host.SettingBool
	case spec.Kind == settingspkg.Int:
		row.Kind, row.Min = host.SettingInt, &spec.Least
		if spec.Most < math.MaxInt32 {
			row.Max = &spec.Most
		}
	case spec.Kind != settingspkg.Text:
		panic(fmt.Sprintf("settings: %s has kind %d, which the wire does not name", spec.Key, spec.Kind))
	case spec.ListOf != nil:
		row.Kind, row.Choices = host.SettingList, spec.ListOf
	case spec.Choices != nil:
		row.Kind, row.Choices = host.SettingChoice, spec.Choices
	case spec.Offers != nil:
		row.Kind, row.Choices, row.Custom = host.SettingChoice, spec.Offers, true
	}
	return row
}

func settingsPage(page cli.Page, report settingsReport) []string {
	rows := make([]cli.Row, len(report.Settings))
	set := 0
	for i, setting := range report.Settings {
		rows[i] = cli.Row{Cells: []string{setting.Key, cmp.Or(fmt.Sprint(setting.Value), "empty"), joinedWhy(setting.Source, setting.Reason)}}
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

func settingsGetVerb(o output.Verb, args []string, store *settingspkg.Store, dir string) int {
	if len(args) != 1 {
		return o.Usage(errors.New("wants one key"))
	}
	spec, known := specByKey(store, args[0])
	if !known {
		return o.Usage(fmt.Errorf("%q is not a declared setting", args[0]))
	}
	setting := resolvedSetting(store, spec, dir)
	return output.Show(o.Out, o.AsJSON, cli.Envelope{Verb: o.Name, OK: true, At: time.Now(), Data: setting},
		func(cli.Page) []string { return []string{fmt.Sprint(setting.Value)} })
}

func settingsSetVerb(o output.Verb, args []string, store *settingspkg.Store, dir string) int {
	scope := settingspkg.Global
	rest := args
	if len(args) >= 2 && args[0] == "--scope" {
		switch args[1] {
		case "global":
		case "project":
			scope = settingspkg.Project
		default:
			return o.Usage(fmt.Errorf("--scope wants global or project, got %q", args[1]))
		}
		rest = args[2:]
	}
	if len(rest) != 2 {
		return o.Usage(errors.New("wants a key and a value"))
	}
	spec, known := specByKey(store, rest[0])
	if !known {
		return o.Usage(fmt.Errorf("%q is not a declared setting", rest[0]))
	}
	before := fmt.Sprint(settingOf(store, spec))
	if before == "" || strings.ContainsAny(before, " \t") {
		before = strconv.Quote(before)
	}
	actor, _ := wiring.BoardyIdentity("")
	written := host.Writing(dir, host.TopicSettings, actor)
	err := writeSetting(store, scope, spec, rest[1])
	written()
	if err != nil {
		return o.Usage(err)
	}
	return o.Receipt(host.WriteReceipt{
		Changes: []host.FileChange{{Change: host.ChangeChanged, What: spec.Key + " = " + rest[1], File: store.Path(scope)}},
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
