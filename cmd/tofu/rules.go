package main

import (
	"cmp"
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
	rulesSubcommands    = "tofu rules list|check|fired|index|overrides|add|off|restore|remove"
	rulesListUsage      = "tofu rules list [--library dir] [--dir project] [--json]"
	rulesOverridesUsage = "tofu rules overrides [--library dir] [--dir project] [--json]"
	rulesCheckUsage     = "tofu rules check [path] [--library dir] [--dir project] [--json]"
)

type rulesFlags struct {
	library, dir string
	json         bool
	rest         []string
}

type ruleListing struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Origin   string           `json:"origin"`
	Mode     string           `json:"mode,omitempty"`
	File     string           `json:"file,omitempty"`
	Override *overrideListing `json:"override,omitempty"`
}

type overrideListing struct {
	RuleID  string `json:"rule_id"`
	Version int    `json:"version,omitempty"`
	Current int    `json:"current,omitempty"`
	Layer   string `json:"layer"`
	Change  string `json:"change"`
	Text    string `json:"text,omitempty"`
	Reason  string `json:"reason,omitempty"`
	By      string `json:"by,omitempty"`
	At      string `json:"at,omitempty"`
	Stale   bool   `json:"stale"`
	File    string `json:"file"`
}

type overridesReport struct {
	Origin    string            `json:"origin"`
	Overrides []overrideListing `json:"overrides"`
}

type layerOverride struct {
	layer string
	rule.Overriding
}

type ruleStack struct {
	rules     []rule.Rule
	origin    string
	under     map[string][]rule.Rule
	overrides []layerOverride
}

func (o layerOverride) listing() *overrideListing {
	r := o.Rule
	listed := &overrideListing{RuleID: cmp.Or(r.Override.Of, r.ID), Version: r.Override.Version, Current: o.Base.Version, Layer: o.layer, Change: "text", Text: r.Text,
		Reason: r.Override.Reason, By: string(r.Override.By), At: r.Override.At, Stale: o.Stale, File: r.File}
	if r.Mode == rule.ModeOff {
		listed.Change, listed.Text = string(rule.ModeOff), ""
	}
	return listed
}

func stackRules(library, project string) (ruleStack, error) {
	rules, origin, err := baseRules(library)
	if err != nil {
		return ruleStack{}, err
	}
	layers, err := userRuleLayers(project)
	if err != nil {
		return ruleStack{}, err
	}
	stack := ruleStack{origin: origin, under: map[string][]rule.Rule{}}
	for _, layer := range layers {
		over, err := layer.load()
		if err != nil {
			return ruleStack{}, err
		}
		stack.under[layer.name] = rules
		for _, found := range rule.Overrides(rules, over) {
			stack.overrides = append(stack.overrides, layerOverride{layer.name, found})
		}
		rules = rule.Layer(rules, over)
	}
	stack.rules = rules
	return stack, nil
}

func (s ruleStack) applied(r rule.Rule) *overrideListing {
	at := slices.IndexFunc(s.overrides, func(o layerOverride) bool { return o.Rule.File == r.File })
	if at < 0 {
		return nil
	}
	return s.overrides[at].listing()
}

func (s ruleStack) switchedOff() []layerOverride {
	return slices.DeleteFunc(slices.Clone(s.overrides), func(o layerOverride) bool { return o.Stale || o.Rule.Mode != rule.ModeOff })
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
	case "overrides":
		return rulesOverridesVerb(args[1:], out, errOut)
	case "restore":
		return rulesRestoreVerb(args[1:], out, errOut)
	default:
		return bare.usage(fmt.Errorf("unknown subcommand %q", args[0]))
	}
}

func loadRules(library, project string) ([]rule.Rule, string, error) {
	stack, err := stackRules(library, project)
	return stack.rules, stack.origin, err
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
	stack, err := stackRules(opts.library, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	layers, err := userRuleLayers(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	listing := make([]ruleListing, len(stack.rules))
	for i, r := range stack.rules {
		listing[i] = ruleListing{ID: r.ID, Kind: string(r.Kind), Origin: "shipped", Override: stack.applied(r)}
		if stack.origin != rulesFromTheBinary {
			listing[i].Origin = "library"
		}
		if r.Checker != "" {
			listing[i].Mode = r.Mode.String()
		}
		if at := slices.IndexFunc(layers, func(layer ruleLayer) bool { return strings.HasPrefix(r.File, layer.dir+string(filepath.Separator)) }); at >= 0 {
			listing[i].Origin, listing[i].File = layers[at].name, r.File
		}
	}
	for _, off := range stack.switchedOff() {
		listing = append(listing, ruleListing{ID: off.Base.ID, Kind: string(off.Base.Kind), Origin: off.layer, Mode: string(rule.ModeOff), File: off.Rule.File, Override: off.listing()})
	}
	report := ruleListReport{Origin: stack.origin, Rules: listing}
	return o.done(true, report, report.lines)
}

func (report ruleListReport) lines(page cli.Page) []string {
	var origins []string
	checks := map[string]int{}
	running := 0
	for _, r := range report.Rules {
		if !slices.Contains(origins, r.Origin) {
			origins = append(origins, r.Origin)
		}
		checks[r.Mode]++
		if r.Mode != string(rule.ModeOff) {
			running++
		}
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
	lines := page.Title("Rules", []string{strconv.Itoa(running) + " run", "from " + page.Path(report.Origin)}, verdict)
	for _, name := range origins {
		var rows []cli.Row
		for _, r := range report.Rules {
			if r.Origin != name {
				continue
			}
			row := cli.Row{Mark: listingMark(rule.Mode(r.Mode)), Cells: []string{r.ID, r.Kind, r.Mode}, Detail: page.Path(r.File)}
			if r.Override != nil {
				row.Detail = r.Override.why()
			}
			rows = append(rows, row)
		}
		lines = append(lines, "", page.Section(name, cli.Verdict{}))
		lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	}
	return lines
}

func (o overrideListing) why() string {
	return "overridden in " + o.Layer + ": " + cmp.Or(o.Reason, "no reason given")
}

func (o overrideListing) ref() string {
	if o.Version == 0 {
		return o.RuleID
	}
	return o.RuleID + "@" + strconv.Itoa(o.Version)
}

func (o overrideListing) staleWhy() string {
	if o.Current == 0 {
		return "stale: no rule " + o.RuleID + " runs below it"
	}
	return "stale: the rule is at @" + strconv.Itoa(o.Current) + " and runs unchanged"
}

func rulesOverridesVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules overrides", usageLine: rulesOverridesUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	opts, err := parseRulesFlags(args)
	if err == nil && len(opts.rest) > 0 {
		err = fmt.Errorf("unknown argument %q", opts.rest[0])
	}
	if err != nil {
		return o.usage(err)
	}
	stack, err := stackRules(opts.library, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	report := overridesReport{Origin: stack.origin, Overrides: make([]overrideListing, len(stack.overrides))}
	for i, found := range stack.overrides {
		report.Overrides[i] = *found.listing()
	}
	return o.done(true, report, report.lines)
}

func (report overridesReport) lines(page cli.Page) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "no overrides"}
	rows := make([]cli.Row, len(report.Overrides))
	stale := 0
	for i, o := range report.Overrides {
		rows[i] = cli.Row{Mark: cli.Changed, Cells: []string{o.ref(), o.Layer, o.Change, cmp.Or(o.Reason, "no reason given")}, Detail: strings.TrimSpace("by " + cmp.Or(o.By, "person") + " " + o.At + " " + page.Path(o.File))}
		if o.Stale {
			stale++
			rows[i].Mark, rows[i].Detail = cli.Warn, o.staleWhy()
		}
	}
	switch {
	case stale > 0:
		verdict = cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(stale) + " stale of " + strconv.Itoa(len(rows))}
	case len(rows) > 0:
		verdict = cli.Verdict{Mark: cli.Changed, Text: plural(len(rows), "override")}
	}
	lines := page.Title("Rule overrides", []string{"from " + page.Path(report.Origin)}, verdict)
	if len(rows) > 0 {
		lines = append(append(lines, ""), page.Rows(rows)...)
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
