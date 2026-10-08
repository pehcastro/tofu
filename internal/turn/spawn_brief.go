package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/subagent"
)

func (s SubAgents) prompt(inherited Config, definition subagent.Definition, task string, owns []string) (string, string, error) {
	system, environment := inherited.System, inherited.Environment
	switch {
	case s.Prompt.Environment != "":
		spec := s.Prompt
		spec.Task, spec.Paths, spec.Agent, spec.Role = task, owns, definition, rule.RoleSubAgent
		owned, err := rule.Frameworks(s.Root, owns)
		if err != nil {
			return "", "", err
		}
		spec.Frameworks = append(slices.Clone(spec.Frameworks), owned...)
		slices.Sort(spec.Frameworks)
		spec.Frameworks = slices.Compact(spec.Frameworks)
		composed, err := Compose(spec)
		if err != nil {
			return "", "", err
		}
		system, environment = composed.Head(), composed.WithTaskRules(spec.Environment)
	case definition.Name != "":
		system += "\n\n" + agentPart(definition).Text
	}
	return system, strings.TrimSpace(environment + "\n\n" + holdingWords(owns)), nil
}

func holdingWords(owns []string) string {
	if len(owns) == 0 {
		return "you were spawned without owns, so you hold no paths: write and edit are refused, a bash command may write only under the temp directory, and what you find goes in your report"
	}
	return "the paths you hold, and the only ones write, edit and bash may change: " + strings.Join(owns, ", ")
}

func (t *SpawnTool) scratch(id string) (string, error) {
	if t.Project == "" {
		return "", nil
	}
	root := filepath.Join(t.Project, ".tofu", "scratch")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	ignore, err := os.OpenFile(filepath.Join(root, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, err = ignore.WriteString("*\n")
		err = errors.Join(err, ignore.Close())
	}
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	made, err := os.MkdirTemp(root, id+"-")
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(t.Project, made)
	return filepath.ToSlash(inside), err
}

func scratchWords(scratch string) string {
	if scratch == "" {
		return ""
	}
	return "\n\nyour scratch folder is " + scratch + ": logs, captures, probes and notes go there, never in the project. " +
		"write, edit and bash write and delete inside it with no owns and with no ask; spell it as this relative path, with no variable."
}

var briefPath = regexp.MustCompile(`[\w./-]*\w\.[A-Za-z0-9]+`)

type briefCandidate struct {
	path   string
	byLead bool
}

func (t *SpawnTool) briefFiles(ctx context.Context, args spawnArgs, lead []llm.Message) string {
	read, readable := t.base.Tools.byName["read"]
	if !readable {
		return ""
	}
	if memoised, cached := read.(interface{ Uncached() Tool }); cached {
		read = memoised.Uncached()
	}
	var candidates []briefCandidate
	for _, path := range briefPath.FindAllString(args.Task, -1) {
		candidates = append(candidates, briefCandidate{path: path})
	}
	for _, message := range lead {
		for _, call := range message.ToolCalls {
			var leadRead readArgs
			if call.Name == "read" && json.Unmarshal(call.Arguments, &leadRead) == nil {
				candidates = append(candidates, briefCandidate{path: leadRead.Path, byLead: true})
			}
		}
	}
	var text strings.Builder
	var placed, skipped []string
	for _, candidate := range candidates {
		raw, _ := json.Marshal(readArgs{Path: candidate.path})
		result, err := read.Run(ctx, raw)
		if err != nil {
			continue
		}
		file := ledgerKey(result.Command)
		if owned, _ := subagent.Matches(file, args.Owns); slices.Contains(placed, file) || candidate.byLead && !owned {
			continue
		}
		placed = append(placed, file)
		body := result.Content
		if repaired := ledgerKey(candidate.path) != file; repaired {
			_, body, _ = strings.Cut(body, "\n")
		}
		if len(skipped) > 0 || text.Len()+len(body) > konst.SubAgentReferenceBytes {
			skipped = append(skipped, file)
			continue
		}
		heading := "the brief names " + file + ", so it is read for you:\n"
		if candidate.byLead {
			heading = "the lead read " + file + ", inside the paths you hold, so it is read for you as it is now:\n"
		}
		text.WriteString("\n\n" + heading + body)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&text, "\n\nthese files, named in the brief or read by the lead inside the paths you hold, are not read for you to stay within %d bytes, so read them before you change them: %s", konst.SubAgentReferenceBytes, strings.Join(skipped, ", "))
	}
	return text.String()
}
