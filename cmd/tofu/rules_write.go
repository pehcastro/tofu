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

	"tofu/internal/rule"
	"tofu/internal/sys"
)

const (
	rulesAddUsage = `tofu rules add [--global] [--dir project] [--replace] [--concern c] [--json] <id> "<text>"`
	rulesIDUsage  = "tofu rules off|remove [--global] [--dir project] [--json] <id>"
)

type ruleLayer struct{ name, dir, flag string }

type ruleWriteOpts struct {
	layer   ruleLayer
	dir     string
	replace bool
	json    bool
	concern rule.Concern
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
	at := slices.IndexFunc(loaded, func(r rule.Rule) bool { return r.ID == id })
	if err != nil || at < 0 {
		return rule.Rule{}, false, err
	}
	return loaded[at], true, nil
}

func parseRuleWriteArgs(args []string) (ruleWriteOpts, error) {
	opts := ruleWriteOpts{dir: "."}
	global := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--concern" || arg == "--dir" {
			if i++; i >= len(args) {
				return ruleWriteOpts{}, fmt.Errorf("%s needs a value", arg)
			}
		}
		switch {
		case arg == "--global":
			global = true
		case arg == "--replace":
			opts.replace = true
		case arg == jsonFlag:
			opts.json = true
		case arg == "--concern":
			opts.concern = rule.Concern(args[i])
		case arg == "--dir":
			opts.dir = args[i]
		case strings.HasPrefix(arg, "-"):
			return ruleWriteOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			opts.rest = append(opts.rest, arg)
		}
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

func ruleRuns(id, project string) (bool, error) {
	running, _, err := loadRules("", project)
	return slices.ContainsFunc(running, func(r rule.Rule) bool { return r.ID == id }), err
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
	data, err := rule.HumanRuleFile(id, cmp.Or(opts.concern, rule.ConcernCodeRules), rule.ModeShadow, text)
	if err != nil {
		return o.usage(err)
	}
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return o.fail(err)
	}
	if found && !opts.replace {
		return o.fail(problemError{What: "the " + opts.layer.name + " rules already carry " + id, Hint: "tofu rules add --replace" + opts.layer.flag + " " + id + " \"<text>\""})
	}
	file := cmp.Or(existing.File, filepath.Join(opts.layer.dir, id+"@1.yaml"))
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.fail(err)
	}
	change := fileChange{Change: changeAdded, What: "rule " + id, File: file}
	if found {
		change.Change = changeChanged
	}
	return o.receipt(writeReceipt{Changes: []fileChange{change}, Undo: "tofu rules remove" + opts.layer.flag + " " + id})
}

func rulesOffVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "rules off", usageLine: rulesIDUsage, out: out, errOut: errOut}
	opts, err := parseRuleIDArgs(args)
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
		return o.fail(problemError{What: "the " + opts.layer.name + " rules already carry " + id, Hint: "tofu rules remove" + opts.layer.flag + " " + id})
	}
	runs, err := ruleRuns(id, opts.dir)
	if err != nil {
		return o.fail(err)
	}
	if !runs {
		return o.fail(problemError{What: "no rule " + id + " runs", Hint: "tofu rules list"})
	}
	data, err := rule.HumanRuleFile(id, rule.ConcernCodeRules, rule.ModeOff, "switched off by tofu rules off")
	if err != nil {
		return o.fail(err)
	}
	file := filepath.Join(opts.layer.dir, id+"@1.yaml")
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.fail(err)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeChanged, What: "rule " + id + " off", File: file}}, Undo: "tofu rules remove" + opts.layer.flag + " " + id})
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
		runs, err := ruleRuns(id, opts.dir)
		if err != nil {
			return o.fail(err)
		}
		if runs {
			return o.fail(problemError{What: id + " has no file in the " + opts.layer.name + " rules", Hint: "tofu rules off" + opts.layer.flag + " " + id})
		}
		return o.fail(problemError{What: "no rule " + id + " in the " + opts.layer.name + " rules", Hint: "tofu rules list"})
	}
	if err := os.Remove(existing.File); err != nil {
		return o.fail(err)
	}
	undo := fmt.Sprintf("tofu rules add%s %s %q", opts.layer.flag, id, existing.Text)
	if existing.Mode == rule.ModeOff {
		undo = "tofu rules off" + opts.layer.flag + " " + id
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "rule " + id, File: existing.File}}, Undo: undo})
}
