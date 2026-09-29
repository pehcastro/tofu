package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/subagent"
	"tofu/internal/sys"
)

type agentLayer struct{ name, parent, agents, flag string }

type agentWriteOpts struct {
	layer                            agentLayer
	dir, description, model, toolCSV string
	json                             bool
	rest                             []string
}

func parseAgentWriteArgs(args []string) (agentWriteOpts, error) {
	opts := agentWriteOpts{dir: "."}
	values := map[string]*string{"--description": &opts.description, "--model": &opts.model, "--tools": &opts.toolCSV, "--dir": &opts.dir}
	global := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		target, takesValue := values[arg]
		switch {
		case takesValue && i+1 < len(args):
			i++
			*target = args[i]
		case takesValue:
			return agentWriteOpts{}, fmt.Errorf("%s needs a value", arg)
		case arg == "--global":
			global = true
		case arg == jsonFlag:
			opts.json = true
		case strings.HasPrefix(arg, "-"):
			return agentWriteOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			opts.rest = append(opts.rest, arg)
		}
	}
	project, err := filepath.Abs(opts.dir)
	if err != nil {
		return agentWriteOpts{}, err
	}
	projectFlag := ""
	if opts.dir != "." {
		projectFlag = " --dir " + project
	}
	opts.dir, opts.layer = project, agentLayer{"project", project, filepath.Join(project, sys.StateDirName, "agents"), projectFlag}
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return agentWriteOpts{}, err
		}
		opts.layer = agentLayer{"global", home, filepath.Join(sys.StateDir(home), "agents"), " --global"}
	}
	return opts, nil
}

func (o agentWriteOpts) named(positional int, fits bool) (string, error) {
	if len(o.rest) != positional || !fits {
		return "", errors.New("missing or extra arguments")
	}
	name := o.rest[0]
	if name == "" || name[0] < 'a' || name[0] > 'z' || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return "", fmt.Errorf("%q is not a plain lower-case name: letters, digits and -, starting with a letter", name)
	}
	return name, nil
}

func (o agentWriteOpts) file(name string) string {
	return filepath.Join(o.layer.agents, name+".md")
}

func knownModel(dir, model string) error {
	if model == string(subagent.RunsInherit) {
		return nil
	}
	catalog, err := modelLibrary(dir)
	if err == nil {
		_, err = catalog.Select(model)
	}
	return err
}

func definitionAt(found subagent.Found, path string) (subagent.Definition, bool) {
	for _, listed := range found.Definitions {
		for _, definition := range append([]subagent.Definition{listed}, listed.Shadowed...) {
			if definition.Path == path {
				return definition, true
			}
		}
	}
	return subagent.Definition{}, false
}

func listedAgent(found subagent.Found, name string) (subagent.Definition, bool) {
	at := slices.IndexFunc(found.Definitions, func(definition subagent.Definition) bool { return definition.Name == name })
	if at < 0 {
		return subagent.Definition{}, false
	}
	return found.Definitions[at], true
}

func agentsAddVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "agents add", usageLine: agentsAddUsage, out: out, errOut: errOut}
	opts, err := parseAgentWriteArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	name, err := opts.named(1, opts.description != "" && opts.model != "")
	if err != nil {
		return o.usage(err)
	}
	if err := knownModel(opts.dir, opts.model); err != nil {
		return o.fail(err)
	}
	var tools []string
	for tool := range strings.SplitSeq(opts.toolCSV, ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			tools = append(tools, tool)
		}
	}
	data, err := subagent.DefinitionFile(name, opts.description, opts.model, tools)
	if err != nil {
		return o.fail(err)
	}
	file := opts.file(name)
	if _, err := os.Stat(file); err == nil {
		return o.fail(problemError{What: "the " + opts.layer.name + " layer already has " + name, Hint: "tofu agents set" + opts.layer.flag + " " + name + " <source/model>"})
	}
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return o.fail(err)
	}
	found, err := agentsIn(opts.dir)
	written, read := definitionAt(found, file)
	switch {
	case err == nil && !read:
		err = fmt.Errorf("tofu does not read %s, because the agentSources setting leaves out tofu", o.path(filepath.Dir(file)))
	case err == nil && written.Runs == subagent.RunsRefused:
		err = errors.New(strings.Join(written.Refused, "; "))
	}
	if err != nil {
		_ = os.Remove(file)
		return o.fail(fmt.Errorf("%s was not added: %w", name, err))
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeAdded, What: "sub-agent " + name, File: file}}, Undo: "tofu agents remove" + opts.layer.flag + " " + name})
}

func agentsSetVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "agents set", usageLine: agentsSetUsage, out: out, errOut: errOut}
	opts, err := parseAgentWriteArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	name, err := opts.named(2, opts.description+opts.model+opts.toolCSV == "")
	if err != nil {
		return o.usage(err)
	}
	model := opts.rest[1]
	found, err := agentsIn(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	before, listed := listedAgent(found, name)
	if !listed {
		return o.fail(problemError{What: "no sub-agent " + name, Hint: "tofu agents"})
	}
	if err := knownModel(opts.dir, model); err != nil {
		return o.fail(err)
	}
	file, err := subagent.Assign(opts.layer.parent, name, model)
	if err != nil {
		return o.fail(err)
	}
	previous := string(subagent.RunsInherit)
	if before.Runs == subagent.RunsModel {
		previous = before.Model
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeChanged, What: "sub-agent " + name + " runs " + model, File: file}}, Undo: "tofu agents set" + opts.layer.flag + " " + name + " " + previous})
}

func agentsRemoveVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "agents remove", usageLine: agentsRemoveUsage, out: out, errOut: errOut}
	opts, err := parseAgentWriteArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	name, err := opts.named(1, opts.description+opts.model+opts.toolCSV == "")
	if err != nil {
		return o.usage(err)
	}
	found, err := agentsIn(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	file := opts.file(name)
	written, ours := definitionAt(found, file)
	listed, isListed := listedAgent(found, name)
	switch {
	case ours:
	case isListed && listed.Origin == "library":
		return o.fail(problemError{What: name + " ships with tofu, so there is no file of yours to remove", Hint: "tofu agents set" + opts.layer.flag + " " + name + " <source/model>"})
	case isListed:
		return o.fail(problemError{What: name + " is read from " + o.path(listed.Path) + ", not the " + opts.layer.name + " layer"})
	default:
		return o.fail(problemError{What: "no sub-agent " + name, Hint: "tofu agents"})
	}
	if err := os.Remove(file); err != nil {
		return o.fail(err)
	}
	changes := []fileChange{{Change: changeRemoved, What: "sub-agent " + name, File: file}}
	assignments, was, err := subagent.Unassign(opts.layer.parent, name)
	if err != nil {
		return o.fail(err)
	}
	if was != "" {
		changes = append(changes, fileChange{Change: changeRemoved, What: "model line " + name + ": " + was, File: assignments})
	}
	undo := fmt.Sprintf("tofu agents add%s %s --description %q --model %s", opts.layer.flag, name, written.Description, written.Written)
	if len(written.Tools) > 0 {
		undo += " --tools " + strings.Join(written.Tools, ",")
	}
	return o.receipt(writeReceipt{Changes: changes, Undo: undo})
}
