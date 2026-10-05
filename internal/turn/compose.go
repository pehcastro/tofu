package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/prompt"
	"tofu/internal/rule"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

const TheFormatContract = prompt.TheFormatContract

type PromptPart struct {
	Concern rule.Concern
	RuleID  string
	Agent   string
	File    string
	Text    string
	ByTask  bool
}

func (p PromptPart) Rule() string {
	switch {
	case p.Agent != "":
		return "the sub-agent " + p.Agent
	case p.RuleID != "":
		return "the rule " + p.RuleID
	}
	return "tofu itself"
}

func (p PromptPart) From() string {
	if p.File == "" {
		return p.Rule()
	}
	return p.Rule() + " in " + p.File
}

type ComposeSpec struct {
	Task         string
	Paths        []string
	Environment  string
	ToolGuidance string
	Rules        []rule.Rule
	SwitchedOff  []rule.Overriding
	Agent        subagent.Definition
	Skills       []skill.Skill
	WindowTokens int
	Role         rule.Role
	Frameworks   []string
}

const concernSkills rule.Concern = "skills"

const referencesOnDemand = "your references, each read whole with the " + referenceToolName + " tool when the work reaches it:"

func agentPart(definition subagent.Definition) PromptPart {
	var text strings.Builder
	text.WriteString("you are the " + definition.Name + " sub-agent, and these are your instructions:\n" + definition.Instructions)
	if len(definition.References) > 0 {
		text.WriteString("\n\n" + referencesOnDemand)
	}
	for _, reference := range definition.References {
		var title string
		var sections []string
		for line := range strings.Lines(reference.Text) {
			line = strings.TrimSpace(line)
			heading, isTitle := strings.CutPrefix(line, "# ")
			section, isSection := strings.CutPrefix(line, "## ")
			switch {
			case isTitle && title == "":
				title = heading
			case isSection:
				sections = append(sections, section)
			}
		}
		text.WriteString("\n- " + reference.Name + ": " + cmp.Or(title, reference.Name))
		if len(sections) > 0 {
			text.WriteString(", on " + strings.Join(sections, "; "))
		}
	}
	if len(definition.Cut) > 0 {
		fmt.Fprintf(&text, "\n\nthese references were cut to keep them within %d bytes and are not here: %s", konst.SubAgentReferenceBytes, strings.Join(definition.Cut, ", "))
	}
	return PromptPart{Concern: rule.ConcernIdentity, Agent: definition.Name, File: filepath.ToSlash(definition.Path), Text: text.String()}
}

func autoloadedSkills(definition subagent.Definition, skills []skill.Skill) (string, error) {
	if definition.Origin == "library" {
		return librarySkills(shipped.Files(), definition)
	}
	var text strings.Builder
	for _, name := range definition.Skills {
		body, err := skill.Load(skills, name, "")
		switch {
		case err == nil:
			text.WriteString("\n\nthe skill " + name + ", loaded before your task:\n" + body)
		case definition.Origin == ".tofu" || definition.Origin == "~/.tofu":
			return "", fmt.Errorf("the sub-agent %s names the skill %s: %w", definition.Name, name, err)
		}
	}
	return text.String(), nil
}

func librarySkills(library fs.FS, definition subagent.Definition) (string, error) {
	var text strings.Builder
	for _, name := range definition.Skills {
		found, _ := fs.Glob(library, "*/skills/"+name+".md")
		deeper, _ := fs.Glob(library, "*/*/skills/"+name+".md")
		if found = append(found, deeper...); len(found) != 1 {
			return "", fmt.Errorf("the library sub-agent %s names the skill %s, which the library ships %d times: %s", definition.Name, name, len(found), strings.Join(found, ", "))
		}
		data, err := fs.ReadFile(library, found[0])
		if err != nil {
			return "", err
		}
		_, body, _ := strings.Cut(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n---\n")
		text.WriteString("\n\nthe skill " + name + ", loaded before your task:\n" + strings.TrimLeft(body, "\n"))
	}
	if text.Len() > konst.SubAgentReferenceBytes {
		return "", fmt.Errorf("the library sub-agent %s loads %d bytes of skills, past the %d a sub-agent is given", definition.Name, text.Len(), konst.SubAgentReferenceBytes)
	}
	return text.String(), nil
}

type Composed struct {
	Task     rule.Task
	Parts    []PromptPart
	HeldBack []rule.Match
}

func composedOrder() []rule.Concern {
	return []rule.Concern{
		rule.ConcernEnvironment,
		rule.ConcernToolGuidance,
		rule.ConcernSafety,
		rule.ConcernOutputShape,
		rule.ConcernFormatContract,
		rule.ConcernCodeRules,
		rule.ConcernProcessDiscipline,
		rule.ConcernDomainKnowledge,
		rule.ConcernTaskShaping,
		rule.ConcernIdentity,
	}
}

func Compose(spec ComposeSpec) (Composed, error) {
	composed := Composed{Task: TaskNamed(spec.Task)}
	composed.Task.Role, composed.Task.Frameworks = spec.Role, spec.Frameworks
	for _, owned := range spec.Paths {
		composed.Task.Paths = append(composed.Task.Paths, filepath.ToSlash(owned))
	}
	if spec.Agent.Name != "" {
		loaded, err := autoloadedSkills(spec.Agent, spec.Skills)
		if err != nil {
			return Composed{}, err
		}
		part := agentPart(spec.Agent)
		part.Text += loaded
		composed.Parts = append(composed.Parts, part)
	}
	if composed.Task.Language = spec.Agent.Language; composed.Task.Language != "" && !rule.KnownLanguage(composed.Task.Language) {
		return Composed{}, fmt.Errorf("the sub-agent %s declares the language %q, which no rule knows", spec.Agent.Name, composed.Task.Language)
	}
	for _, builtin := range []PromptPart{
		{Concern: rule.ConcernEnvironment, Text: spec.Environment},
		{Concern: rule.ConcernToolGuidance, Text: spec.ToolGuidance},
		{Concern: rule.ConcernFormatContract, Text: TheFormatContract},
	} {
		if strings.TrimSpace(builtin.Text) == "" {
			return Composed{}, fmt.Errorf("the composed prompt carries nothing for %s, which tofu writes itself and is never conditional", builtin.Concern)
		}
		composed.Parts = append(composed.Parts, builtin)
	}
	agentDomain := cmp.Or(spec.Agent.Domain, rule.DomainDev)
	reaching := slices.DeleteFunc(slices.Clone(spec.Rules), func(loaded rule.Rule) bool {
		reaches, _ := loaded.ReachesDomain(agentDomain, composed.Task)
		return !reaches || loaded.Mode == rule.ModeOff
	})
	withoutTask := rule.Index(reaching, rule.Task{Role: composed.Task.Role, Language: composed.Task.Language, Frameworks: composed.Task.Frameworks})
	for i, match := range rule.Index(reaching, composed.Task) {
		if !match.Fires {
			composed.HeldBack = append(composed.HeldBack, match)
			continue
		}
		fired := reaching[i]
		reachesWithoutTask, _ := fired.ReachesDomain(agentDomain, rule.Task{})
		text := cmp.Or(strings.TrimSpace(fired.Text), strings.TrimSpace(fired.Notes))
		if text == "" {
			return Composed{}, fmt.Errorf("rule %s fires, %s, and carries no text the prompt can send: %s",
				fired.ID, match.Why, filepath.ToSlash(fired.File))
		}
		composed.Parts = append(composed.Parts, PromptPart{
			Concern: fired.Concern,
			RuleID:  fired.ID,
			File:    filepath.ToSlash(fired.File),
			Text:    text,
			ByTask:  !withoutTask[i].Fires || !reachesWithoutTask,
		})
	}
	for _, off := range spec.SwitchedOff {
		text := "the person switched this rule off on purpose, so it does not apply here"
		if off.Rule.Override.Reason != "" {
			text += ": " + off.Rule.Override.Reason
		}
		composed.Parts = append(composed.Parts, PromptPart{Concern: off.Base.Concern, RuleID: off.Base.ID, File: filepath.ToSlash(off.Rule.File), Text: text})
	}
	order := composedOrder()
	slices.SortStableFunc(composed.Parts, func(a, b PromptPart) int {
		return slices.Index(order, a.Concern) - slices.Index(order, b.Concern)
	})
	if listing := skill.Listing(spec.Skills, spec.WindowTokens); listing != "" {
		composed.Parts = append(composed.Parts, PromptPart{Concern: concernSkills, Text: listing})
	}
	return composed, nil
}

func (c Composed) System() string {
	return c.render(func(PromptPart) bool { return true })
}

func (c Composed) Head() string {
	return c.render(func(part PromptPart) bool { return !part.ByTask })
}

func (c Composed) WithTaskRules(environment string) string {
	return strings.TrimSpace(environment + "\n\n" + c.render(func(part PromptPart) bool { return part.ByTask }))
}

func (c Composed) render(keep func(PromptPart) bool) string {
	var block strings.Builder
	for _, part := range c.Parts {
		if part.Concern == rule.ConcernEnvironment || !keep(part) {
			continue
		}
		if block.Len() > 0 {
			block.WriteString("\n\n")
		}
		fmt.Fprintf(&block, "[%s, from %s]\n%s", part.Concern, part.Rule(), part.Text)
	}
	return block.String()
}

func TaskNamed(text string) rule.Task {
	task := rule.Task{Text: text}
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, ",;:()[]{}\"'`")
		if strings.ContainsAny(word, `/\`) || namedExtension(word) {
			task.Paths = append(task.Paths, filepath.ToSlash(word))
			continue
		}
		if task.Verb == rule.VerbNone {
			task.Verb = verbNamed(word)
		}
	}
	return task
}

func namedExtension(word string) bool {
	extension := strings.TrimPrefix(path.Ext(word), ".")
	if len(extension) < 2 {
		return false
	}
	for _, letter := range extension {
		if !unicode.IsLetter(letter) {
			return false
		}
	}
	return true
}

func verbNamed(word string) rule.Verb {
	named := rule.Verb(strings.ToLower(word))
	if slices.Contains([]rule.Verb{rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite}, named) {
		return named
	}
	return rule.VerbNone
}

const referenceToolName = "reference"

type referenceTool struct {
	held map[string]string
}

func (referenceTool) Name() string { return referenceToolName }

func (referenceTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        referenceToolName,
		Description: "returns one of your references whole, named as your instructions list it, such as go-verify",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
	}
}

func (r referenceTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("reference: arguments are not the expected shape: %w", err)
	}
	text, held := r.held[args.Name]
	if !held {
		return Result{}, fmt.Errorf("reference: no reference is named %q; the references are %s", args.Name, strings.Join(slices.Sorted(maps.Keys(r.held)), ", "))
	}
	return Result{Content: text, Command: referenceToolName + " " + args.Name}, nil
}
