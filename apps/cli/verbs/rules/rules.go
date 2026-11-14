package rules

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/subagents"
	"tofu/internal/host"
	"tofu/internal/librarian/rules"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/project/boardy"
)

const (
	subcommandsUsage = "tofu rules list|show|check|fired|index|overrides|add|off|restore|remove|why"
	listUsage        = "tofu rules list [--library dir] [--dir project] [--json]"
	overridesUsage   = "tofu rules overrides [--library dir] [--dir project] [--json]"
	checkUsage       = "tofu rules check [path] [--library dir] [--dir project] [--json]"
	Usage            = subcommandsUsage + " " + listUsage + " " + overridesUsage + " " + checkUsage + " " + addUsage + " " + offUsage + " " + idUsage + " " + showUsage
)

type Deps struct {
	Agents func(dir string) (subagents.Found, error)
	Why    func(args []string, out, errOut io.Writer) int
}

type flags struct {
	library, dir string
	json         bool
	rest         []string
}

type OverridesReport struct {
	Origin    string                 `json:"origin"`
	Overrides []host.OverrideListing `json:"overrides"`
}

func Verb(deps Deps) func(ctx context.Context, args []string, out, errOut io.Writer) int {
	return func(ctx context.Context, args []string, out, errOut io.Writer) int {
		bare := output.Verb{Name: "rules", UsageLine: subcommandsUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
		if len(output.WithoutJSON(args)) == 0 {
			return bare.Usage(errors.New("no subcommand"))
		}
		switch args[0] {
		case "add":
			return AddVerb(args[1:], out, errOut)
		case "off":
			return offVerb(args[1:], out, errOut)
		case "remove":
			return removeVerb(args[1:], out, errOut)
		case "show":
			return showVerb(args[1:], out, errOut)
		case "list":
			return listVerb(args[1:], out, errOut)
		case "check":
			return checkVerb(ctx, args[1:], out, errOut)
		case "fired":
			return firedVerb(args[1:], out, errOut)
		case "index":
			return indexVerb(args[1:], out, errOut, deps.Agents)
		case "overrides":
			return overridesVerb(args[1:], out, errOut)
		case "restore":
			return restoreVerb(args[1:], out, errOut)
		case "why":
			return deps.Why(args[1:], out, errOut)
		default:
			return bare.Usage(fmt.Errorf("unknown subcommand %q", args[0]))
		}
	}
}

func listedOrigin(stack wiring.RuleStack, r rules.Rule) string {
	if stack.Origin == wiring.RulesFromTheBinary || stack.Origin == wiring.RulesFromTheProject && strings.HasPrefix(r.File, wiring.LibraryRoot+"/") {
		return "shipped"
	}
	return "library"
}

func ruleOrigin(stack wiring.RuleStack, layers []wiring.RuleLayer, r rules.Rule) string {
	inLayer := func(layer wiring.RuleLayer) bool {
		return strings.HasPrefix(r.File, layer.Dir+string(filepath.Separator))
	}
	if at := slices.IndexFunc(layers, inLayer); at >= 0 {
		return layers[at].Name
	}
	return listedOrigin(stack, r)
}

func listingMark(mode rules.Mode) cli.Mark {
	switch mode {
	case "":
		return cli.Done
	case rules.ModeEnforced:
		return cli.Active
	case rules.ModeShadow:
		return cli.Idle
	case rules.ModeOff:
		return cli.Removed
	}
	panic("tofu: unknown rule mode " + string(mode))
}

func listVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules list", UsageLine: listUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseFlags(args)
	if err == nil && len(opts.rest) > 0 {
		err = fmt.Errorf("unknown argument %q", opts.rest[0])
	}
	if err != nil {
		return o.Usage(err)
	}
	boards, opened := boardy.OpenStore(opts.dir)
	stack, err := wiring.StackRules(opts.library, boards, opened)
	if err != nil {
		return o.Fail(err)
	}
	layers, err := wiring.UserRuleLayers(opts.dir)
	if err != nil {
		return o.Fail(err)
	}
	listing := make([]host.RuleListing, len(stack.Rules))
	var checked []string
	for i, r := range stack.Rules {
		if r.Checker != "" {
			checked = append(checked, r.ID)
		}
		listing[i] = host.RuleListing{ID: r.ID, Kind: string(r.Kind), Origin: listedOrigin(stack, r), Mode: r.Mode.String(), Switch: wiring.ShadowCheck(r), Override: stack.Applied(r)}
		if slices.Contains(stack.BySetting, r.ID) {
			listing[i].Switch = "on by the setting " + wiring.RuleSetting(r.ID)
		}
		if origin := ruleOrigin(stack, layers, r); origin != listing[i].Origin {
			listing[i].Origin, listing[i].File = origin, r.File
		}
	}
	for _, off := range stack.SwitchedOff() {
		listing = append(listing, host.RuleListing{ID: off.Base.ID, Kind: string(off.Base.Kind), Origin: off.Layer, Mode: string(rules.ModeOff), File: off.Rule.File, Override: off.Listing()})
	}
	for _, off := range stack.ShippedOff {
		listing = append(listing, host.RuleListing{ID: off.ID, Kind: string(off.Kind), Origin: listedOrigin(stack, off), Mode: string(rules.ModeOff), Switch: stack.SwitchOn(off.ID)})
	}
	if opts.library == "" {
		library, points := wiring.ReadLibrary(platformproject.ResolvePlaces("."))
		if library.Unreadable != "" {
			return o.Fail(errors.New(library.Unreadable))
		}
		for _, point := range points {
			listing = append(listing, wiring.PointListing(point))
		}
	}
	report := host.RuleListReport{Origin: stack.Origin, Rules: listing}
	return o.Done(true, report, func(page cli.Page) []string { return listLines(page, report, checked) })
}

func listLines(page cli.Page, report host.RuleListReport, checked []string) []string {
	checkMode := func(r host.RuleListing) rules.Mode {
		if r.Mode == string(rules.ModeOff) || r.Kind == wiring.PointKind || slices.Contains(checked, r.ID) {
			return rules.Mode(r.Mode)
		}
		return ""
	}
	var origins []string
	checks := map[rules.Mode]int{}
	running, points := 0, 0
	for _, r := range report.Rules {
		if !slices.Contains(origins, r.Origin) {
			origins = append(origins, r.Origin)
		}
		if r.Kind == wiring.PointKind {
			points++
			continue
		}
		checks[checkMode(r)]++
		if r.Mode != string(rules.ModeOff) {
			running++
		}
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "no checks"}
	var counts []string
	for _, mode := range []rules.Mode{rules.ModeShadow, rules.ModeEnforced} {
		if count := checks[mode]; count > 0 {
			verdict.Mark = listingMark(mode)
			counts = append(counts, strconv.Itoa(count)+" "+mode.String())
		}
	}
	if len(counts) > 0 {
		verdict.Text = strings.Join(counts, " · ")
	}
	facts := []string{strconv.Itoa(running) + " run", "from " + page.Path(report.Origin)}
	if points > 0 {
		facts = slices.Insert(facts, 1, output.Plural(points, "point"))
	}
	lines := page.Title("Rules", facts, verdict)
	for _, name := range origins {
		var rows []cli.Row
		for _, r := range report.Rules {
			if r.Origin != name {
				continue
			}
			mode := checkMode(r)
			row := cli.Row{Mark: listingMark(mode), Cells: []string{r.ID, r.Kind, string(mode)}, Detail: cmp.Or(r.Switch, page.Path(r.File))}
			if r.Override != nil {
				row.Detail = wiring.OverrideWhy(*r.Override)
			}
			if r.Kind == wiring.PointKind && r.Mode == "" {
				row.Mark = cli.Fail
			}
			rows = append(rows, row)
		}
		lines = append(lines, "", page.Section(name, cli.Verdict{}))
		lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	}
	return lines
}

func overridesVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules overrides", UsageLine: overridesUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseFlags(args)
	if err == nil && len(opts.rest) > 0 {
		err = fmt.Errorf("unknown argument %q", opts.rest[0])
	}
	if err != nil {
		return o.Usage(err)
	}
	boards, opened := boardy.OpenStore(opts.dir)
	stack, err := wiring.StackRules(opts.library, boards, opened)
	if err != nil {
		return o.Fail(err)
	}
	report := OverridesReport{Origin: stack.Origin, Overrides: make([]host.OverrideListing, len(stack.Overrides))}
	for i, found := range stack.Overrides {
		report.Overrides[i] = *found.Listing()
	}
	return o.Done(true, report, report.lines)
}

func (report OverridesReport) lines(page cli.Page) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "no overrides"}
	rows := make([]cli.Row, len(report.Overrides))
	stale := 0
	for i, o := range report.Overrides {
		rows[i] = cli.Row{Mark: cli.Changed, Cells: []string{wiring.OverrideRef(o), o.Layer, o.Change, cmp.Or(o.Reason, "no reason given")}, Detail: strings.TrimSpace("by " + cmp.Or(o.By, "person") + " " + o.At + " " + page.Path(o.File))}
		if o.Stale {
			stale++
			rows[i].Mark, rows[i].Detail = cli.Warn, wiring.OverrideStaleWhy(o)
		}
	}
	switch {
	case stale > 0:
		verdict = cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(stale) + " stale of " + strconv.Itoa(len(rows))}
	case len(rows) > 0:
		verdict = cli.Verdict{Mark: cli.Changed, Text: output.Plural(len(rows), "override")}
	}
	lines := page.Title("Rule overrides", []string{"from " + page.Path(report.Origin)}, verdict)
	if len(rows) > 0 {
		lines = append(append(lines, ""), page.Rows(rows)...)
	}
	return lines
}

func parseFlags(args []string) (flags, error) {
	parsed := flags{dir: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--library" || arg == "--dir" {
			if i++; i >= len(args) {
				return flags{}, fmt.Errorf("%s needs a directory", arg)
			}
		}
		switch {
		case arg == output.JSONFlag:
			parsed.json = true
		case arg == "--library":
			parsed.library = args[i]
		case arg == "--dir":
			parsed.dir = args[i]
		case strings.HasPrefix(arg, "-"):
			return flags{}, fmt.Errorf("unknown argument %q", arg)
		default:
			parsed.rest = append(parsed.rest, arg)
		}
	}
	return parsed, nil
}
