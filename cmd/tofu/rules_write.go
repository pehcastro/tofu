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

type ruleLayer struct{ name, dir, flag string }

type ruleWriteOpts struct {
	layer   ruleLayer
	dir     string
	replace bool
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

func parseRuleIDArgs(verb string, args []string) (ruleWriteOpts, error) {
	opts, err := parseRuleWriteArgs(args)
	if err == nil && (len(opts.rest) != 1 || opts.replace || opts.concern != "") {
		err = fmt.Errorf("usage: tofu rules %s [--global] [--dir project] <id>", verb)
	}
	return opts, err
}

func ruleRuns(id, project string) (bool, error) {
	running, _, err := loadRules("", project)
	return slices.ContainsFunc(running, func(r rule.Rule) bool { return r.ID == id }), err
}

func printRuleChange(out io.Writer, what, file, undo string) int {
	_, _ = fmt.Fprintf(out, "%s\nfile: %s\nundo: %s\n", what, file, undo)
	return exitOK
}

func rulesAddVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRuleWriteArgs(args)
	if err == nil && len(opts.rest) != 2 {
		err = errors.New(`usage: tofu rules add [--global] [--dir project] [--replace] [--concern c] <id> "<text>"`)
	}
	if err != nil {
		return rulesFail(errOut, err)
	}
	id, text := opts.rest[0], opts.rest[1]
	data, err := rule.HumanRuleFile(id, cmp.Or(opts.concern, rule.ConcernCodeRules), rule.ModeShadow, text)
	if err != nil {
		return rulesFail(errOut, err)
	}
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return rulesFail(errOut, err)
	}
	if found && !opts.replace {
		return rulesFail(errOut, fmt.Errorf("the %s rules already carry %s in %s, add --replace to write over it", opts.layer.name, id, existing.File))
	}
	file := cmp.Or(existing.File, filepath.Join(opts.layer.dir, id+"@1.yaml"))
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return rulesFail(errOut, err)
	}
	what := fmt.Sprintf("added %s to the %s rules", id, opts.layer.name)
	if found {
		what += ", replacing: " + existing.Text
	}
	return printRuleChange(out, what, file, "tofu rules remove"+opts.layer.flag+" "+id)
}

func rulesOffVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRuleIDArgs("off", args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	id := opts.rest[0]
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return rulesFail(errOut, err)
	}
	if found {
		return rulesFail(errOut, fmt.Errorf("the %s rules already carry %s in %s, and tofu rules remove%s %s deletes that file", opts.layer.name, id, existing.File, opts.layer.flag, id))
	}
	runs, err := ruleRuns(id, opts.dir)
	if err != nil {
		return rulesFail(errOut, err)
	}
	if !runs {
		return rulesFail(errOut, fmt.Errorf("no rule %s runs, so there is nothing to switch off. tofu rules list names every rule that runs", id))
	}
	data, err := rule.HumanRuleFile(id, rule.ConcernCodeRules, rule.ModeOff, "switched off by tofu rules off")
	if err != nil {
		return rulesFail(errOut, err)
	}
	file := filepath.Join(opts.layer.dir, id+"@1.yaml")
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return rulesFail(errOut, err)
	}
	return printRuleChange(out, fmt.Sprintf("switched %s off in the %s rules", id, opts.layer.name), file, "tofu rules remove"+opts.layer.flag+" "+id)
}

func rulesRemoveVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRuleIDArgs("remove", args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	id := opts.rest[0]
	existing, found, err := opts.layer.find(id)
	if err != nil {
		return rulesFail(errOut, err)
	}
	if !found {
		runs, err := ruleRuns(id, opts.dir)
		if err != nil {
			return rulesFail(errOut, err)
		}
		if runs {
			return rulesFail(errOut, fmt.Errorf("%s has no file in the %s rules, so there is nothing to remove there. tofu rules off%s %s switches it off", id, opts.layer.name, opts.layer.flag, id))
		}
		return rulesFail(errOut, fmt.Errorf("no rule %s is in the %s rules", id, opts.layer.name))
	}
	if err := os.Remove(existing.File); err != nil {
		return rulesFail(errOut, err)
	}
	undo := fmt.Sprintf("tofu rules add%s %s %q", opts.layer.flag, id, existing.Text)
	if existing.Mode == rule.ModeOff {
		undo = "tofu rules off" + opts.layer.flag + " " + id
	}
	return printRuleChange(out, fmt.Sprintf("removed %s from the %s rules", id, opts.layer.name), existing.File, undo)
}
