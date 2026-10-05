package main

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

	"tofu/internal/rule"
	"tofu/internal/sys"
)

const (
	rulesAddUsage = `tofu rules add [--global|--project] [--dir project] [--replace] [--concern c] [--json] <id> "<text>" [--reason "<why>"]`
	rulesOffUsage = `tofu rules off [--global|--project] [--dir project] [--json] <id> --reason "<why>"`
	rulesIDUsage  = "tofu rules remove|restore [--global|--project] [--dir project] [--json] <id>"
)

type ruleLayer struct{ name, dir, flag string }

type ruleWriteOpts struct {
	layer   ruleLayer
	dir     string
	replace bool
	json    bool
	concern rule.Concern
	reason  string
	rest    []string
}

func userRuleLayers(project string) ([]ruleLayer, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	full, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	projectFlag := ""
	if project != "." {
		projectFlag = " --dir " + full
	}
	return []ruleLayer{{"global", filepath.Join(home, "rules"), " --global"}, {"project", filepath.Join(sys.StateDir(full), "rules"), projectFlag}}, nil
}

func (l ruleLayer) load() ([]rule.Rule, error) {
	isDir, err := sys.IsDir(l.dir)
	if err != nil || !isDir {
		return nil, err
	}
	return rule.LoadDir(l.dir)
}

func (l ruleLayer) find(id string) (rule.Rule, bool, error) {
	loaded, err := l.load()
	at := slices.IndexFunc(loaded, func(r rule.Rule) bool { return r.ID == id || r.Override.Of == id })
	if err != nil || at < 0 {
		return rule.Rule{}, false, err
	}
	return loaded[at], true, nil
}

func (l ruleLayer) again(r rule.Rule) string {
	switch {
	case r.Override.Of != "" && r.Mode == rule.ModeOff:
		return fmt.Sprintf("tofu rules off%s %s --reason %q", l.flag, r.Override.Of, r.Override.Reason)
	case r.Override.Of != "":
		return fmt.Sprintf("tofu rules add%s %s %q --reason %q", l.flag, r.ID, r.Text, r.Override.Reason)
	case r.Mode == rule.ModeOff:
		return "tofu rules off" + l.flag + " " + r.ID + ` --reason "<why>"`
	}
	return fmt.Sprintf("tofu rules add%s --concern %s %s %q", l.flag, r.Concern, r.ID, r.Text)
}

func parseRuleWriteArgs(args []string) (ruleWriteOpts, error) {
	opts := ruleWriteOpts{dir: "."}
	global, project := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--concern" || arg == "--dir" || arg == "--reason" {
			if i++; i >= len(args) {
				return ruleWriteOpts{}, fmt.Errorf("%s needs a value", arg)
			}
		}
		switch {
		case arg == "--global":
			global = true
		case arg == "--project":
			project = true
		case arg == "--replace":
			opts.replace = true
		case arg == jsonFlag:
			opts.json = true
		case arg == "--concern":
			opts.concern = rule.Concern(args[i])
		case arg == "--dir":
			opts.dir = args[i]
		case arg == "--reason":
			opts.reason = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "-"):
			return ruleWriteOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			opts.rest = append(opts.rest, arg)
		}
	}
	if global && project {
		return ruleWriteOpts{}, errors.New("--global or --project, not both")
	}
	layers, err := userRuleLayers(opts.dir)
	if err != nil {
		return ruleWriteOpts{}, err
	}
	opts.layer = layers[1]
	if global {
		opts.layer = layers[0]
	}
	return opts, nil
}

func parseRuleIDArgs(args []string) (ruleWriteOpts, error) {
	opts, err := parseRuleWriteArgs(args)
	if err == nil && (len(opts.rest) != 1 || opts.replace || opts.concern != "") {
		err = errors.New("one id, and no --replace or --concern")
	}
	return opts, err
}

func (opts ruleWriteOpts) under(id string) (rule.Rule, bool, error) {
	stack, err := stackRules("", opts.dir)
	below := stack.under[opts.layer.name]
	at := slices.IndexFunc(below, func(r rule.Rule) bool { return r.ID == id })
	if err != nil || at < 0 {
		return rule.Rule{}, false, err
	}
	return below[at], true, nil
}

func (opts ruleWriteOpts) override(base rule.Rule, text string) ([]byte, error) {
	return rule.OverrideFile(base.ID, text, rule.Override{Of: base.ID, Version: base.Version, Reason: opts.reason, By: rule.ByPerson, At: time.Now().Format(time.DateOnly)})
}

func rulesAddVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules add", usageLine: rulesAddUsage, out: out, errOut: errOut}
	opts, err := parseRuleWriteArgs(args)
	if err == nil && len(opts.rest) != 2 {
		err = errors.New("an id and its text")
	}
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	id, text := opts.rest[0], opts.rest[1]
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return o.fail(err)
	}
	if found && !opts.replace {
		return o.fail(problemError{What: "the " + opts.layer.name + " rules already carry " + id, Hint: "tofu rules add --replace" + opts.layer.flag + " " + id + " \"<text>\""})
	}
	base, overrides, err := opts.under(id)
	if err != nil {
		return o.fail(err)
	}
	var data []byte
	switch {
	case overrides && opts.reason == "":
		return o.fail(problemError{What: id + " is a rule tofu runs already, and an override of it says why", Hint: "tofu rules add" + opts.layer.flag + " " + id + ` "<text>" --reason "<why>"`})
	case overrides && opts.concern != "":
		return o.usage(fmt.Errorf("an override keeps the concern of %s, so --concern does not apply", id))
	case overrides:
		data, err = opts.override(base, text)
	case opts.reason != "":
		return o.usage(fmt.Errorf("--reason is for overriding a rule that runs, and no rule %s runs", id))
	default:
		data, err = rule.HumanRuleFile(id, cmp.Or(opts.concern, rule.ConcernCodeRules), rule.ModeShadow, text)
	}
	if err != nil {
		return o.usage(err)
	}
	file := cmp.Or(existing.File, filepath.Join(opts.layer.dir, id+"@1.yaml"))
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.fail(err)
	}
	change := fileChange{Change: changeAdded, What: "rule " + id, File: file}
	undo := "tofu rules remove" + opts.layer.flag + " " + id
	if overrides {
		undo = "tofu rules restore" + opts.layer.flag + " " + id
	}
	switch {
	case found && existing.Mode == rule.ModeOff:
		change.Change, undo = changeChanged, undo+"; "+opts.layer.again(existing)
	case found:
		change.Change, undo = changeChanged, strings.Replace(opts.layer.again(existing), "tofu rules add", "tofu rules add --replace", 1)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{change}, Undo: undo})
}

func rulesOffVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules off", usageLine: rulesOffUsage, out: out, errOut: errOut}
	opts, err := parseRuleIDArgs(args)
	if err == nil && opts.reason == "" {
		err = errors.New("--reason says why, so a person reading the project later knows")
	}
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	id := opts.rest[0]
	_, found, err := opts.layer.find(id)
	if err != nil {
		return o.fail(err)
	}
	if found {
		return o.fail(problemError{What: "the " + opts.layer.name + " rules already carry " + id, Hint: "tofu rules restore" + opts.layer.flag + " " + id})
	}
	base, runs, err := opts.under(id)
	if err != nil {
		return o.fail(err)
	}
	if !runs {
		return o.fail(problemError{What: "no rule " + id + " runs", Hint: "tofu rules list"})
	}
	data, err := opts.override(base, "")
	if err != nil {
		return o.fail(err)
	}
	file := filepath.Join(opts.layer.dir, id+"@1.yaml")
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.fail(err)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeChanged, What: "rule " + id + " off", File: file}}, Undo: "tofu rules restore" + opts.layer.flag + " " + id})
}

func rulesRemoveVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules remove", usageLine: rulesIDUsage, out: out, errOut: errOut}
	opts, err := parseRuleIDArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	id := opts.rest[0]
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return o.fail(err)
	}
	if !found {
		_, runs, err := opts.under(id)
		if err != nil {
			return o.fail(err)
		}
		if runs {
			return o.fail(problemError{What: id + " has no file in the " + opts.layer.name + " rules", Hint: "tofu rules off" + opts.layer.flag + " " + id + ` --reason "<why>"`})
		}
		return o.fail(problemError{What: "no rule " + id + " in the " + opts.layer.name + " rules", Hint: "tofu rules list"})
	}
	if err := os.Remove(existing.File); err != nil {
		return o.fail(err)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "rule " + existing.ID, File: existing.File}}, Undo: opts.layer.again(existing)})
}

func rulesRestoreVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules restore", usageLine: rulesIDUsage, out: out, errOut: errOut}
	opts, err := parseRuleIDArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	id := opts.rest[0]
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return o.fail(err)
	}
	if !found {
		return o.fail(problemError{What: "the " + opts.layer.name + " rules carry no override of " + id, Hint: "tofu rules overrides"})
	}
	_, overrides, err := opts.under(cmp.Or(existing.Override.Of, id))
	if err != nil {
		return o.fail(err)
	}
	if existing.Override.Of == "" && !overrides {
		return o.fail(problemError{What: id + " overrides nothing, it is a rule of your own", Hint: "tofu rules remove" + opts.layer.flag + " " + id})
	}
	if err := os.Remove(existing.File); err != nil {
		return o.fail(err)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "override of " + id, File: existing.File}}, Undo: opts.layer.again(existing)})
}
