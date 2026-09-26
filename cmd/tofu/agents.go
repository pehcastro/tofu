package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"tofu/internal/llm/models"
	settingspkg "tofu/internal/settings"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	shipped "tofu/library"
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
	store, err := openSettings(dir)
	if err != nil {
		return subagent.Found{}, err
	}
	layers, err := models.Layers(shipped.Files(), dir)
	if err != nil {
		return subagent.Found{}, err
	}
	catalog, _ := models.Load(layers)
	bash, err := turn.NewBashTool(dir)
	if err != nil {
		return subagent.Found{}, err
	}
	built, _, err := assembleRunTools(dir, "", false, bash)
	if err != nil {
		return subagent.Found{}, err
	}
	tools := make([]string, 0, len(built))
	for _, tool := range built {
		tools = append(tools, tool.Name())
	}
	home, _ := os.UserHomeDir()
	library, _, _ := librarySource()
	return subagent.Definitions(subagent.Scan{
		Project: dir,
		Home:    home,
		Sources: strings.Split(store.Text(settingspkg.AgentSources), ","),
		Library: library,
		Tools:   tools,
		Catalog: catalog,
	}), nil
}

func printAgents(out io.Writer, found subagent.Found) {
	for _, definition := range found.Definitions {
		model := string(definition.Runs)
		if definition.Runs == subagent.RunsModel {
			model = definition.Model
		}
		_, _ = fmt.Fprintf(out, "%-16s %-28s %-9s %s\n  %s\n", definition.Name, model, definition.Origin, definition.Path, definition.Description)
		if len(definition.Tools) > 0 {
			_, _ = fmt.Fprintln(out, "  tools "+strings.Join(definition.Tools, ", "))
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
