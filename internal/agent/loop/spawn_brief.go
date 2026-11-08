package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	platformscratch "tofu/internal/platform/scratch"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/read"
)

func (s SubAgents) prompt(inherited Config, definition subagents.Definition, task string, owns []string) (string, []PromptPart, string, error) {
	system, environment := inherited.System, inherited.Environment
	var parts []PromptPart
	switch {
	case s.Prompt.Environment != "":
		spec := s.Prompt
		spec.Task, spec.Paths, spec.Agent, spec.Role = task, owns, definition, rules.RoleSubAgent
		reached, err := rules.Frameworks(s.Root, slices.Concat(owns, TaskNamed(task).Paths))
		if err != nil {
			return "", nil, "", err
		}
		spec.Frameworks = append(slices.Clone(spec.Frameworks), reached...)
		slices.Sort(spec.Frameworks)
		spec.Frameworks = slices.Compact(spec.Frameworks)
		composed, err := Compose(spec)
		if err != nil {
			return "", nil, "", err
		}
		system, parts, environment = composed.Head(), composed.Recorded(), composed.WithTaskRules(spec.Environment)
	case definition.Name != "":
		system, parts = system+"\n\n"+agentPart(definition).Text, referenceParts(definition)
	}
	return system, parts, Redated(strings.TrimSpace(environment+"\n\n"+holdingWords(owns)), inherited.Now), nil
}

func holdingWords(owns []string) string {
	if len(owns) == 0 {
		return "you were spawned without owns, so you hold no paths: write, edit and bash write only inside your scratch folder, and what you find goes in your report"
	}
	return "the paths you hold, and the only ones write, edit and bash may change: " + strings.Join(owns, ", ")
}

func WithLeadScratch(ctx context.Context, config Config) (context.Context, error) {
	place, err := leadScratch(config.Project, config.Sessions, config.Session)
	if err != nil {
		return ctx, err
	}
	return platformscratch.WithScratch(ctx, place), place.Make()
}

func leadScratch(project string, sessions *session.Store, id string) (platformscratch.ScratchPlace, error) {
	name, tag := "", session.FamilyTag(id)
	if sessions != nil && id != "" {
		if identity, err := sessions.Identity(id); err == nil {
			name, tag = identity.Name, identity.Tag
		}
	}
	return platformscratch.ScratchFor(project, id, name, tag)
}

func (t *SpawnTool) scratch(ctx context.Context, id string) (platformscratch.ScratchPlace, error) {
	lead, found := platformscratch.ScratchOf(ctx)
	if !found && t.Project == "" {
		return platformscratch.ScratchPlace{}, nil
	}
	if !found {
		var err error
		if lead, err = leadScratch(t.Project, t.base.Sessions, t.base.Session); err != nil {
			return platformscratch.ScratchPlace{}, err
		}
	}
	place := lead.For(id)
	return place, place.Make()
}

func scratchWords(place platformscratch.ScratchPlace) string {
	if place.Root == "" {
		return ""
	}
	return "\n\nyour scratch folder is " + place.Dir() + ", outside the project: probes, logs, captures and notes go there, never in the project. " +
		"TMPDIR names its tmp folder in every command you run, and build caches sit under " + place.Cache() + ". " +
		"write, edit and bash write and delete inside your folder with no owns and with no ask; spell it as this absolute path or as $TMPDIR. " +
		"read the shared notes in " + place.Shared() + ", and call scratch_path when you need a folder by kind."
}

var memoryCitation = platformfs.LazyRegexp(`\[memory#([\w-]+)\]`)

func (t *SpawnTool) citedMemory(brief string) (string, error) {
	cited := memoryCitation().FindAllStringSubmatch(brief, -1)
	if len(cited) == 0 {
		return "", nil
	}
	var remembered map[string]string
	if t.Remembered != nil {
		var err error
		if remembered, err = t.Remembered(); err != nil {
			return "", fmt.Errorf("spawn refused: the brief cites memory and the memory did not open: %w", err)
		}
	}
	var lines []string
	for _, citation := range cited {
		line, known := remembered[citation[1]]
		if !known {
			return "", fmt.Errorf("spawn refused: %s is no memory entry of yours; cite an id from your memory, or write what the sub-agent needs into the task", citation[0])
		}
		if !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	return "memory the lead cited for you, each line as it is saved:\n" + strings.Join(lines, "\n") + "\n\n", nil
}

var briefPath = platformfs.LazyRegexp(`[\w./-]*\w\.[A-Za-z0-9]+`)

type briefCandidate struct {
	path   string
	byLead bool
}

func (t *SpawnTool) briefFiles(ctx context.Context, args spawnArgs, lead []core.Message) string {
	reader, readable := t.base.Tools.byName["read"]
	if !readable {
		return ""
	}
	if memoised, cached := reader.(interface{ Uncached() contract.Tool }); cached {
		reader = memoised.Uncached()
	}
	named := briefPath().FindAllString(args.Task, -1)
	if direct, isRead := reader.(*read.ReadTool); isRead {
		named = contract.RealPaths(direct.Root(), named)
	}
	var candidates []briefCandidate
	listed := map[string]bool{}
	for _, path := range named {
		listed[contract.LedgerKey(path)] = true
		candidates = append(candidates, briefCandidate{path: path})
	}
	for _, message := range lead {
		for _, call := range message.ToolCalls {
			var leadRead read.ReadArgs
			if call.Name != "read" || json.Unmarshal(call.Arguments, &leadRead) != nil {
				continue
			}
			key := contract.LedgerKey(leadRead.Path)
			if owned, _ := subagents.Matches(key, args.Owns); owned && !listed[key] {
				candidates = append(candidates, briefCandidate{path: leadRead.Path, byLead: true})
			}
			listed[key] = true
		}
	}
	var text strings.Builder
	var placed, skipped []string
	for _, candidate := range candidates {
		raw, _ := json.Marshal(read.ReadArgs{Path: candidate.path})
		result, err := reader.Run(read.Uncounted(ctx), raw)
		if err != nil {
			continue
		}
		file := contract.LedgerKey(result.Command)
		if owned, _ := subagents.Matches(file, args.Owns); slices.Contains(placed, file) || candidate.byLead && !owned {
			continue
		}
		placed = append(placed, file)
		body := result.Content
		if repaired := contract.LedgerKey(candidate.path) != file; repaired {
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
