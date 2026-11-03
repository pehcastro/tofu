package rules

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/wiring"
	"tofu/internal/host"
	"tofu/internal/librarian/rules"
	"tofu/internal/sys"
)

const (
	addUsage = `tofu rules add [--global|--project] [--dir project] [--replace] [--concern c] [--json] <id> "<text>" [--reason "<why>"]`
	offUsage = `tofu rules off [--global|--project] [--dir project] [--json] <id> --reason "<why>"`
	idUsage  = "tofu rules remove|restore [--global|--project] [--dir project] [--json] <id>"
)

type writeOpts struct {
	layer   wiring.RuleLayer
	dir     string
	replace bool
	json    bool
	concern rule.Concern
	reason  string
	rest    []string
}

func parseWriteArgs(args []string) (writeOpts, error) {
	opts := writeOpts{dir: "."}
	global, project := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--concern" || arg == "--dir" || arg == "--reason" {
			if i++; i >= len(args) {
				return writeOpts{}, fmt.Errorf("%s needs a value", arg)
			}
		}
		switch {
		case arg == "--global":
			global = true
		case arg == "--project":
			project = true
		case arg == "--replace":
			opts.replace = true
		case arg == output.JSONFlag:
			opts.json = true
		case arg == "--concern":
			opts.concern = rule.Concern(args[i])
		case arg == "--dir":
			opts.dir = args[i]
		case arg == "--reason":
			opts.reason = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "-"):
			return writeOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			opts.rest = append(opts.rest, arg)
		}
	}
	if global && project {
		return writeOpts{}, errors.New("--global or --project, not both")
	}
	layers, err := wiring.UserRuleLayers(opts.dir)
	if err != nil {
		return writeOpts{}, err
	}
	opts.layer = layers[1]
	if global {
		opts.layer = layers[0]
	}
	return opts, nil
}

func parseIDArgs(args []string) (writeOpts, error) {
	opts, err := parseWriteArgs(args)
	if err == nil && (len(opts.rest) != 1 || opts.replace || opts.concern != "") {
		err = errors.New("one id, and no --replace or --concern")
	}
	return opts, err
}

func (opts writeOpts) under(id string) (rule.Rule, bool, error) {
	stack, err := wiring.StackRules("", opts.dir)
	below := stack.Under[opts.layer.Name]
	at := slices.IndexFunc(below, func(r rule.Rule) bool { return r.ID == id })
	if err != nil || at < 0 {
		return rule.Rule{}, false, err
	}
	return below[at], true, nil
}

func (opts writeOpts) override(base rule.Rule, text string) ([]byte, error) {
	return rule.OverrideFile(base.ID, text, rule.Override{Of: base.ID, Version: base.Version, Reason: opts.reason, By: rule.ByPerson, At: time.Now().Format(time.DateOnly)})
}

func AddVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules add", UsageLine: addUsage, Out: out, ErrOut: errOut}
	opts, err := parseWriteArgs(args)
	if err == nil && len(opts.rest) != 2 {
		err = errors.New("an id and its text")
	}
	if err != nil {
		return o.Usage(err)
	}
	o.AsJSON = opts.json
	id, text := opts.rest[0], opts.rest[1]
	existing, found, err := opts.layer.Find(id)
	if err != nil {
		return o.Fail(err)
	}
	if found && !opts.replace {
		return o.Fail(output.ProblemError{What: "the " + opts.layer.Name + " rules already carry " + id, Hint: "tofu rules add --replace" + opts.layer.Flag + " " + id + " \"<text>\""})
	}
	base, overrides, err := opts.under(id)
	if err != nil {
		return o.Fail(err)
	}
	var data []byte
	switch {
	case overrides && opts.reason == "":
		return o.Fail(output.ProblemError{What: id + " is a rule tofu runs already, and an override of it says why", Hint: "tofu rules add" + opts.layer.Flag + " " + id + ` "<text>" --reason "<why>"`})
	case overrides && opts.concern != "":
		return o.Usage(fmt.Errorf("an override keeps the concern of %s, so --concern does not apply", id))
	case overrides:
		data, err = opts.override(base, text)
	case opts.reason != "":
		return o.Usage(fmt.Errorf("--reason is for overriding a rule that runs, and no rule %s runs", id))
	default:
		data, err = rule.HumanRuleFile(id, cmp.Or(opts.concern, rule.ConcernCodeRules), rule.ModeShadow, text)
	}
	if err != nil {
		return o.Usage(err)
	}
	file := cmp.Or(existing.File, filepath.Join(opts.layer.Dir, id+"@1.yaml"))
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.Fail(err)
	}
	change := host.FileChange{Change: host.ChangeAdded, What: "rule " + id, File: file}
	undo := "tofu rules remove" + opts.layer.Flag + " " + id
	if overrides {
		undo = "tofu rules restore" + opts.layer.Flag + " " + id
	}
	switch {
	case found && existing.Mode == rule.ModeOff:
		change.Change, undo = host.ChangeChanged, undo+"; "+opts.layer.Again(existing)
	case found:
		change.Change, undo = host.ChangeChanged, strings.Replace(opts.layer.Again(existing), "tofu rules add", "tofu rules add --replace", 1)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{change}, Undo: undo})
}

func offVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules off", UsageLine: offUsage, Out: out, ErrOut: errOut}
	opts, err := parseIDArgs(args)
	if err == nil && opts.reason == "" {
		err = errors.New("--reason says why, so a person reading the project later knows")
	}
	if err != nil {
		return o.Usage(err)
	}
	o.AsJSON = opts.json
	id := opts.rest[0]
	_, found, err := opts.layer.Find(id)
	if err != nil {
		return o.Fail(err)
	}
	if found {
		return o.Fail(output.ProblemError{What: "the " + opts.layer.Name + " rules already carry " + id, Hint: "tofu rules restore" + opts.layer.Flag + " " + id})
	}
	base, runs, err := opts.under(id)
	if err != nil {
		return o.Fail(err)
	}
	if !runs {
		return o.Fail(output.ProblemError{What: "no rule " + id + " runs", Hint: "tofu rules list"})
	}
	if base.Mode == rule.ModeOff {
		hint := "tofu rules list"
		if key := wiring.RuleSetting(id); key != "" {
			hint = "tofu settings set " + key + " false"
		}
		return o.Fail(output.ProblemError{What: id + " ships off, and nothing in the " + opts.layer.Name + " rules turns it on", Hint: hint})
	}
	data, err := opts.override(base, "")
	if err != nil {
		return o.Fail(err)
	}
	file := filepath.Join(opts.layer.Dir, id+"@1.yaml")
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.Fail(err)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeChanged, What: "rule " + id + " off", File: file}}, Undo: "tofu rules restore" + opts.layer.Flag + " " + id})
}

func removeVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules remove", UsageLine: idUsage, Out: out, ErrOut: errOut}
	opts, err := parseIDArgs(args)
	if err != nil {
		return o.Usage(err)
	}
	o.AsJSON = opts.json
	id := opts.rest[0]
	existing, found, err := opts.layer.Find(id)
	if err != nil {
		return o.Fail(err)
	}
	if !found {
		_, runs, err := opts.under(id)
		if err != nil {
			return o.Fail(err)
		}
		if runs {
			return o.Fail(output.ProblemError{What: id + " has no file in the " + opts.layer.Name + " rules", Hint: "tofu rules off" + opts.layer.Flag + " " + id + ` --reason "<why>"`})
		}
		return o.Fail(output.ProblemError{What: "no rule " + id + " in the " + opts.layer.Name + " rules", Hint: "tofu rules list"})
	}
	if err := os.Remove(existing.File); err != nil {
		return o.Fail(err)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeRemoved, What: "rule " + existing.ID, File: existing.File}}, Undo: opts.layer.Again(existing)})
}

func restoreVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules restore", UsageLine: idUsage, Out: out, ErrOut: errOut}
	opts, err := parseIDArgs(args)
	if err != nil {
		return o.Usage(err)
	}
	o.AsJSON = opts.json
	id := opts.rest[0]
	existing, found, err := opts.layer.Find(id)
	if err != nil {
		return o.Fail(err)
	}
	if !found {
		return switchOn(o, opts, id)
	}
	_, overrides, err := opts.under(cmp.Or(existing.Override.Of, id))
	if err != nil {
		return o.Fail(err)
	}
	if existing.Override.Of == "" && !overrides {
		return o.Fail(output.ProblemError{What: id + " overrides nothing, it is a rule of your own", Hint: "tofu rules remove" + opts.layer.Flag + " " + id})
	}
	if err := os.Remove(existing.File); err != nil {
		return o.Fail(err)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeRemoved, What: "override of " + id, File: existing.File}}, Undo: opts.layer.Again(existing)})
}

func switchOn(o output.Verb, opts writeOpts, id string) int {
	base, found, err := opts.under(id)
	if err != nil {
		return o.Fail(err)
	}
	if !found || base.Mode != rule.ModeOff {
		return o.Fail(output.ProblemError{What: "the " + opts.layer.Name + " rules carry no override of " + id, Hint: "tofu rules overrides"})
	}
	data, err := rule.SwitchOnFile(id, rule.Override{Of: id, Version: base.Version, Reason: cmp.Or(opts.reason, "switched on with tofu rules restore"), By: rule.ByPerson, At: time.Now().Format(time.DateOnly)})
	if err != nil {
		return o.Usage(err)
	}
	file := filepath.Join(opts.layer.Dir, id+"@1.yaml")
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.Fail(err)
	}
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeAdded, What: "rule " + id + " on", File: file}}, Undo: "tofu rules remove" + opts.layer.Flag + " " + id})
}
