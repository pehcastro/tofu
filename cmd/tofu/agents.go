package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"tofu/internal/subagent"
	"tofu/internal/turn"
)

const agentsUsage = "usage: tofu agents [--json]"

func agentsVerb(args []string, out, errOut io.Writer) int {
	asJSON := slices.Equal(args, []string{"--json"})
	if len(args) > 0 && !asJSON {
		_, _ = fmt.Fprintln(errOut, "tofu agents: "+agentsUsage)
		return exitUsage
	}
	found, err := discoverAgents()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu agents: %v\n", err)
		return exitUsage
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(found)
	} else {
		printAgents(out, found)
	}
	if len(found.Broken) > 0 {
		return exitVerdict
	}
	return exitOK
}

func discoverAgents() (subagent.Found, error) {
	dir, err := os.Getwd()
	if err != nil {
		return subagent.Found{}, err
	}
	named, _, err := assembleRunTools(dir, toolSetFull, nil, &turn.BashTool{})
	if err != nil {
		return subagent.Found{}, err
	}
	return scanSubAgents(dir, named), nil
}

func printAgents(out io.Writer, found subagent.Found) {
	for _, definition := range found.Definitions {
		model := string(definition.Runs)
		if definition.Runs == subagent.RunsModel {
			model = definition.Model
		}
		_, _ = fmt.Fprintf(out, "%-16s %-28s %-17s %-9s %s\n  %s\n", definition.Name, model, definition.From, definition.Origin, definition.Path, definition.Description)
		if len(definition.Tools) > 0 {
			_, _ = fmt.Fprintln(out, "  tools "+strings.Join(definition.Tools, ", "))
		}
		for _, reference := range definition.References {
			_, _ = fmt.Fprintf(out, "  reference %s, %d bytes, %s\n", reference.Name, len(reference.Text), reference.Path)
		}
		if len(definition.Cut) > 0 {
			_, _ = fmt.Fprintln(out, "  cut to stay within the reference budget: "+strings.Join(definition.Cut, ", "))
		}
		for _, notice := range definition.Notices {
			_, _ = fmt.Fprintln(out, "  notice: "+notice)
		}
		if len(definition.IgnoredTools) > 0 {
			_, _ = fmt.Fprintln(out, "  ignores "+strings.Join(definition.IgnoredTools, ", "))
		}
		for _, reason := range definition.Refused {
			_, _ = fmt.Fprintln(out, "  refused: "+reason)
		}
		for _, shadowed := range definition.Shadowed {
			_, _ = fmt.Fprintln(out, "  shadows "+shadowed.Path)
		}
	}
	for _, broken := range found.Broken {
		_, _ = fmt.Fprintf(out, "broken %s: %s\n", broken.Path, broken.Reason)
	}
	for _, notice := range found.Notices {
		_, _ = fmt.Fprintln(out, "notice: "+notice)
	}
}
