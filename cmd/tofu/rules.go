package main

import (
	"encoding/json"
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
	"tofu/internal/rule"
	"tofu/internal/sys"
	shipped "tofu/library"
)

const (
	libraryRoot         = "library"
	rulesFireSuffix     = ".rules.jsonl"
	rulesFromTheBinary  = "the binary"
	rulesFromTheProject = "the project"
	rulesSubcommands    = "tofu rules list|check|fired|index|add|off|remove"
	rulesListUsage      = "tofu rules list [--library dir] [--dir project] [--json]"
	rulesCheckUsage     = "tofu rules check [path] [--library dir] [--dir project] [--json]"
)

type rulesFlags struct {
	library, dir string
	json         bool
	rest         []string
}

type ruleListing struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
	Mode   string `json:"mode,omitempty"`
	File   string `json:"file,omitempty"`
}

type ruleFireListing struct {
	RuleID   string `json:"rule_id"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Blocked  bool   `json:"blocked"`
	Findings int    `json:"findings"`
}

type ruleFireRecord struct {
	ruleFireListing
	At time.Time `json:"at"`
}

func fireListing(f rule.Fire) ruleFireListing {
	return ruleFireListing{RuleID: f.RuleID, Target: filepath.ToSlash(f.Target), Mode: f.Mode.String(), Blocked: f.Blocked, Findings: len(f.Findings)}
}

type ruleCheckReport struct {
	Origin  string            `json:"origin"`
	Fires   []ruleFireListing `json:"fires"`
	Blocked int               `json:"blocked"`
}

type ruleListReport struct {
	Origin string        `json:"origin"`
	Rules  []ruleListing `json:"rules"`
}

func rulesVerb(args []string, out, errOut io.Writer) int {
	bare := verbOutput{verb: "rules", usageLine: rulesSubcommands, asJSON: jsonAsked(args), out: out, errOut: errOut}
	if len(withoutJSON(args)) == 0 {
		return bare.usage(errors.New("no subcommand"))
	}
	switch args[0] {
	case "add":
		return rulesAddVerb(args[1:], out, errOut)
	case "off":
		return rulesOffVerb(args[1:], out, errOut)
	case "remove":
		return rulesRemoveVerb(args[1:], out, errOut)
	case "list":
		return rulesListVerb(args[1:], out, errOut)
	case "check":
		return rulesCheckVerb(args[1:], out, errOut)
	case "fired":
		return rulesFiredVerb(args[1:], out, errOut)
	case "index":
		return rulesIndexVerb(args[1:], out, errOut)
	default:
		return bare.usage(fmt.Errorf("unknown subcommand %q", args[0]))
	}
}

func loadRules(override, project string) ([]rule.Rule, string, error) {
	rules, origin, err := baseRules(override)
	if err != nil {
		return nil, "", err
	}
	layers, err := userRuleLayers(project)
	if err != nil {
		return nil, "", err
	}
	for _, layer := range layers {
		over, err := layer.load()
		if err != nil {
			return nil, "", err
		}
		rules = rule.Layer(rules, over)
	}
	return rules, origin, nil
}

func baseRules(override string) ([]rule.Rule, string, error) {
	if override != "" {
		rules, err := rule.LoadDir(override)
		return rules, override, err
	}
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		return nil, "", err
	}
	isDir, err := sys.IsDir(libraryDir)
	if err != nil {
		return nil, "", err
	}
	if isDir {
		rules, err := rule.LoadDir(libraryDir)
		return rules, rulesFromTheProject, err
	}
	rules, err := rule.LoadFS(shipped.Files(), libraryRoot)
	return rules, rulesFromTheBinary, err
}

func listingMark(mode rule.Mode) cli.Mark {
	switch mode {
	case "":
		return cli.Done
	case rule.ModeEnforced:
		return cli.Active
	case rule.ModeShadow:
		return cli.Idle
	case rule.ModeOff:
		return cli.Removed
	}
	panic("tofu: unknown rule mode " + string(mode))
}

func rulesListVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules list", usageLine: rulesListUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	opts, err := parseRulesFlags(args)
	if err == nil && len(opts.rest) > 0 {
		err = fmt.Errorf("unknown argument %q", opts.rest[0])
	}
	if err != nil {
		return o.usage(err)
	}
	rules, origin, err := loadRules(opts.library, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	layers, err := userRuleLayers(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	listing := make([]ruleListing, len(rules))
	for i, r := range rules {
		listing[i] = ruleListing{ID: r.ID, Kind: string(r.Kind), Origin: "shipped"}
		if origin != rulesFromTheBinary {
			listing[i].Origin = "library"
		}
		if r.Checker != "" {
			listing[i].Mode = r.Mode.String()
		}
		if at := slices.IndexFunc(layers, func(layer ruleLayer) bool { return strings.HasPrefix(r.File, layer.dir+string(filepath.Separator)) }); at >= 0 {
			listing[i].Origin, listing[i].File = layers[at].name, r.File
		}
	}
	report := ruleListReport{Origin: origin, Rules: listing}
	return o.done(true, report, report.lines)
}

func (report ruleListReport) lines(page cli.Page) []string {
	var origins []string
	checks := map[string]int{}
	for _, r := range report.Rules {
		if !slices.Contains(origins, r.Origin) {
			origins = append(origins, r.Origin)
		}
		checks[r.Mode]++
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "no checks"}
	var counts []string
	for _, mode := range []rule.Mode{rule.ModeShadow, rule.ModeEnforced} {
		if count := checks[mode.String()]; count > 0 {
			verdict.Mark = listingMark(mode)
			counts = append(counts, strconv.Itoa(count)+" "+mode.String())
		}
	}
	if len(counts) > 0 {
		verdict.Text = strings.Join(counts, " · ")
	}
	lines := page.Title("Rules", []string{strconv.Itoa(len(report.Rules)) + " run", "from " + page.Path(report.Origin)}, verdict)
	for _, name := range origins {
		var rows []cli.Row
		for _, r := range report.Rules {
			if r.Origin == name {
				rows = append(rows, cli.Row{Mark: listingMark(rule.Mode(r.Mode)), Cells: []string{r.ID, r.Kind, r.Mode}, Detail: page.Path(r.File)})
			}
		}
		lines = append(lines, "", page.Section(name, cli.Verdict{}))
		lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	}
	return lines
}

func rulesCheckVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules check", usageLine: rulesCheckUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	opts, err := parseRulesFlags(args)
	if err == nil && len(opts.rest) > 1 {
		err = fmt.Errorf("one path, got %q and %q", opts.rest[0], opts.rest[1])
	}
	if err != nil {
		return o.usage(err)
	}
	rules, origin, err := loadRules(opts.library, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	roots := sourceRoots()
	if len(opts.rest) == 1 {
		roots = opts.rest
	}

	var fires []rule.Fire
	for _, root := range roots {
		present, err := sys.Exists(root)
		if err != nil {
			return o.fail(err)
		}
		if !present {
			continue
		}
		found, err := rule.CheckTree(rules, rule.Builtins(), root, time.Now)
		if err != nil {
			return o.fail(err)
		}
		fires = append(fires, found...)
	}

	logDir, err := sys.LogDir()
	if err != nil {
		return o.fail(err)
	}
	report := ruleCheckReport{Origin: origin, Fires: make([]ruleFireListing, len(fires))}
	for i, f := range fires {
		if err := appendRuleFire(logDir, f); err != nil {
			return o.fail(err)
		}
		report.Fires[i] = fireListing(f)
		if f.Blocked {
			report.Blocked++
		}
	}
	return o.done(report.Blocked == 0, report, report.lines)
}

func (report ruleCheckReport) lines(page cli.Page) []string {
	lines := page.Title("Rules check", []string{"from " + page.Path(report.Origin)}, firesVerdict(len(report.Fires), report.Blocked))
	rows := make([]cli.Row, len(report.Fires))
	for i, f := range report.Fires {
		rows[i] = f.row(page, plural(f.Findings, "finding"))
	}
	if len(rows) > 0 {
		lines = append(append(lines, ""), page.Rows(rows)...)
	}
	return lines
}

func firesVerdict(fired, blocked int) cli.Verdict {
	switch {
	case fired == 0:
		return cli.Verdict{Mark: cli.Done, Text: "nothing fired"}
	case blocked == 0:
		return cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(fired) + " fired"}
	}
	return cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(fired) + " fired · " + strconv.Itoa(blocked) + " blocked"}
}

func (f ruleFireListing) row(page cli.Page, detail string) cli.Row {
	mark := cli.Warn
	if f.Blocked {
		mark = cli.Fail
	}
	return cli.Row{Mark: mark, Cells: []string{f.RuleID, page.Path(f.Target), f.Mode}, Detail: detail}
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

func appendRuleFire(dir string, f rule.Fire) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, f.At.UTC().Format("2006-01-02")+rulesFireSuffix)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	line, err := json.Marshal(ruleFireRecord{ruleFireListing: fireListing(f), At: f.At})
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return err
}

func parseRulesFlags(args []string) (rulesFlags, error) {
	flags := rulesFlags{dir: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--library" || arg == "--dir" {
			if i++; i >= len(args) {
				return rulesFlags{}, fmt.Errorf("%s needs a directory", arg)
			}
		}
		switch {
		case arg == jsonFlag:
			flags.json = true
		case arg == "--library":
			flags.library = args[i]
		case arg == "--dir":
			flags.dir = args[i]
		case strings.HasPrefix(arg, "-"):
			return rulesFlags{}, fmt.Errorf("unknown argument %q", arg)
		default:
			flags.rest = append(flags.rest, arg)
		}
	}
	return flags, nil
}
