package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
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
		return "you were spawned without owns, so you hold no paths: write, edit and bash write only inside your scratch folder, and what you find goes in your report"
	}
	return "the paths you hold, and the only ones write, edit and bash may change: " + strings.Join(owns, ", ")
}

func WithLeadScratch(ctx context.Context, config Config) (context.Context, error) {
	place, err := leadScratch(config.Project, config.Sessions, config.Session)
	if err != nil {
		return ctx, err
	}
	return sys.WithScratch(ctx, place), place.Make()
}

func leadScratch(project string, sessions *session.Store, id string) (sys.ScratchPlace, error) {
	name, tag := "", session.FamilyTag(id)
	if sessions != nil && id != "" {
		if identity, err := sessions.Identity(id); err == nil {
			name, tag = identity.Name, identity.Tag
		}
	}
	return sys.ScratchFor(project, id, name, tag)
}

func (t *SpawnTool) scratch(ctx context.Context, id string) (sys.ScratchPlace, error) {
	lead, found := sys.ScratchOf(ctx)
	if !found && t.Project == "" {
		return sys.ScratchPlace{}, nil
	}
	if !found {
		var err error
		if lead, err = leadScratch(t.Project, t.base.Sessions, t.base.Session); err != nil {
			return sys.ScratchPlace{}, err
		}
	}
	place := lead.For(id)
	return place, place.Make()
}

func scratchWords(place sys.ScratchPlace) string {
	if place.Root == "" {
		return ""
	}
	return "\n\nyour scratch folder is " + place.Dir() + ", outside the project: probes, logs, captures and notes go there, never in the project. " +
		"TMPDIR names its tmp folder in every command you run, and build caches sit under " + place.Cache() + ". " +
		"write, edit and bash write and delete inside your folder with no owns and with no ask; spell it as this absolute path or as $TMPDIR. " +
		"read the shared notes in " + place.Shared() + ", and call scratch_path when you need a folder by kind."
}

var memoryCitation = regexp.MustCompile(`\[memory#([\w-]+)\]`)

func (t *SpawnTool) citedMemory(brief string) (string, error) {
	cited := memoryCitation.FindAllStringSubmatch(brief, -1)
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
