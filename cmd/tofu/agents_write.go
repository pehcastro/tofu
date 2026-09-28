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

func (o agentWriteOpts) named(usage string, positional int, fits bool) (string, error) {
	if len(o.rest) != positional || !fits {
		return "", errors.New("usage: " + usage)
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

func agentsRefuse(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu agents: %v\n", err)
	return exitVerdict
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

func printAgentChange(out io.Writer, found subagent.Found, name, what, file, undo string) int {
	printRuleChange(out, what, file, undo)
	if definition, listed := listedAgent(found, name); listed {
		printAgents(out, subagent.Found{Definitions: []subagent.Definition{definition}})
	}
	return exitOK
}

func agentsAddVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseAgentWriteArgs(args)
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	name, err := opts.named(agentsAddUsage, 1, opts.description != "" && opts.model != "")
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	if err := knownModel(opts.dir, opts.model); err != nil {
		return agentsRefuse(errOut, err)
	}
	var tools []string
	for tool := range strings.SplitSeq(opts.toolCSV, ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			tools = append(tools, tool)
		}
	}
	data, err := subagent.DefinitionFile(name, opts.description, opts.model, tools)
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	file := opts.file(name)
	if _, err := os.Stat(file); err == nil {
		return agentsRefuse(errOut, fmt.Errorf("the %s layer already has %s in %s. tofu agents set%s %s <source/model> changes its model", opts.layer.name, name, file, opts.layer.flag, name))
	}
	if err := sys.WriteFile(file, data, 0o644); err != nil {
		return agentsRefuse(errOut, err)
	}
	found, err := agentsIn(opts.dir)
	written, read := definitionAt(found, file)
	switch {
	case err == nil && !read:
		err = fmt.Errorf("tofu does not read %s, because the agentSources setting leaves out tofu", filepath.Dir(file))
	case err == nil && written.Runs == subagent.RunsRefused:
		err = errors.New(strings.Join(written.Refused, "; "))
	}
	if err != nil {
		_ = os.Remove(file)
		return agentsRefuse(errOut, fmt.Errorf("%s was not added: %w", name, err))
	}
	return printAgentChange(out, found, name, fmt.Sprintf("added %s to the %s sub-agents", name, opts.layer.name), file, "tofu agents remove"+opts.layer.flag+" "+name)
}

func agentsSetVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseAgentWriteArgs(args)
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	name, err := opts.named(agentsSetUsage, 2, opts.description+opts.model+opts.toolCSV == "")
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	model := opts.rest[1]
	found, err := agentsIn(opts.dir)
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	before, listed := listedAgent(found, name)
	if !listed {
		return agentsRefuse(errOut, fmt.Errorf("no sub-agent %s is listed. tofu agents names every one", name))
	}
	if err := knownModel(opts.dir, model); err != nil {
		return agentsRefuse(errOut, err)
	}
	file, err := subagent.Assign(opts.layer.parent, name, model)
	if err == nil {
		found, err = agentsIn(opts.dir)
	}
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	previous := string(subagent.RunsInherit)
	if before.Runs == subagent.RunsModel {
		previous = before.Model
	}
	return printAgentChange(out, found, name, fmt.Sprintf("set %s to run %s in the %s agent models", name, model, opts.layer.name), file, "tofu agents set"+opts.layer.flag+" "+name+" "+previous)
}

func agentsRemoveVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseAgentWriteArgs(args)
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	name, err := opts.named(agentsRemoveUsage, 1, opts.description+opts.model+opts.toolCSV == "")
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	found, err := agentsIn(opts.dir)
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	file := opts.file(name)
	written, ours := definitionAt(found, file)
	listed, isListed := listedAgent(found, name)
	switch {
	case ours:
	case isListed && listed.Origin == "library":
		return agentsRefuse(errOut, fmt.Errorf("%s ships with tofu, so there is no file of yours to remove. tofu agents set%s %s <source/model> changes the model it runs", name, opts.layer.flag, name))
	case isListed:
		return agentsRefuse(errOut, fmt.Errorf("%s has no file in the %s layer. It is read from %s", name, opts.layer.name, listed.Path))
	default:
		return agentsRefuse(errOut, fmt.Errorf("no sub-agent %s is listed. tofu agents names every one", name))
	}
	if err := os.Remove(file); err != nil {
		return agentsRefuse(errOut, err)
	}
	what := fmt.Sprintf("removed %s from the %s sub-agents", name, opts.layer.name)
	assignments, was, err := subagent.Unassign(opts.layer.parent, name)
	if err == nil {
		found, err = agentsIn(opts.dir)
	}
	if err != nil {
		return agentsRefuse(errOut, err)
	}
	if was != "" {
		what += fmt.Sprintf(", and its line %s: %s from %s", name, was, assignments)
	}
	undo := fmt.Sprintf("tofu agents add%s %s --description %q --model %s", opts.layer.flag, name, written.Description, written.Written)
	if len(written.Tools) > 0 {
		undo += " --tools " + strings.Join(written.Tools, ",")
	}
	return printAgentChange(out, found, name, what, file, undo)
}
