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
	"regexp"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/prompt"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/skill"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

const TheFormatContract = prompt.TheFormatContract

type PromptPart struct {
	Concern rule.Concern
	RuleID  string
	Agent   string
	Name    string
	File    string
	Text    string
	Why     string
	State   session.PartState
	ByTask  bool
	Off     bool
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
	Dropped      []PromptPart
}

const (
	ConcernSkills     rule.Concern = "skills"
	ConcernReferences rule.Concern = "references"
)

const SkillToolName = "skill"

const referencesOnDemand = "your references, each read whole with the " + ReferenceToolName + " tool when the work reaches it:"

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

func referenceParts(definition subagent.Definition) []PromptPart {
	var parts []PromptPart
	for _, reference := range definition.References {
		parts = append(parts, PromptPart{Concern: ConcernReferences, Agent: definition.Name, Name: reference.Name, File: reference.Path, State: session.PartOffered})
	}
	for _, reference := range definition.Unreached {
		parts = append(parts, PromptPart{Concern: ConcernReferences, Agent: definition.Name, Name: reference.Name, File: reference.Path, State: session.PartDropped,
			Why: "the reference is for " + reference.Framework + ", which no manifest the task reaches lists"})
	}
	for _, name := range definition.Cut {
		parts = append(parts, PromptPart{Concern: ConcernReferences, Agent: definition.Name, Name: name, State: session.PartDropped,
			Why: fmt.Sprintf("cut to keep the references within %d bytes", konst.SubAgentReferenceBytes)})
	}
	return parts
}

func autoloadedSkills(definition subagent.Definition, skills []skill.Skill) (string, []PromptPart, error) {
	var loaded []PromptPart
	if definition.Origin == "library" {
		text, err := librarySkills(shipped.Files(), definition)
		if err != nil {
			return "", nil, err
		}
		for _, name := range definition.Skills {
			loaded = append(loaded, PromptPart{Concern: ConcernSkills, Agent: definition.Name, Name: name, File: "library/" + librarySkillFiles(shipped.Files(), name)[0], State: session.PartLoaded,
				Why: "the library sub-agent's definition names it"})
		}
		return text, loaded, nil
	}
	var text strings.Builder
	for _, name := range definition.Skills {
		body, err := skill.Load(skills, name, "")
		switch {
		case err == nil:
			text.WriteString("\n\nthe skill " + name + ", loaded before your task:\n" + body)
			at := slices.IndexFunc(skills, func(one skill.Skill) bool { return one.Name == name })
			loaded = append(loaded, PromptPart{Concern: ConcernSkills, Agent: definition.Name, Name: name, File: filepath.ToSlash(skills[at].File), State: session.PartLoaded,
				Why: "the sub-agent's definition names it"})
		case definition.Origin == ".tofu" || definition.Origin == "~/.tofu":
			return "", nil, fmt.Errorf("the sub-agent %s names the skill %s: %w", definition.Name, name, err)
		}
	}
	return text.String(), loaded, nil
}

func librarySkillFiles(library fs.FS, name string) []string {
	found, _ := fs.Glob(library, "*/skills/"+name+".md")
	deeper, _ := fs.Glob(library, "*/*/skills/"+name+".md")
	return append(found, deeper...)
}

func librarySkills(library fs.FS, definition subagent.Definition) (string, error) {
	var text strings.Builder
	for _, name := range definition.Skills {
		found := librarySkillFiles(library, name)
		if len(found) != 1 {
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

func skillParts(skills []skill.Skill, skillTool bool, loaded []PromptPart) []PromptPart {
	var parts []PromptPart
	for _, one := range skills {
		part := PromptPart{Concern: ConcernSkills, Name: one.Name, File: filepath.ToSlash(one.File), State: session.PartOffered}
		switch {
		case slices.ContainsFunc(loaded, func(loaded PromptPart) bool { return loaded.Name == one.Name }):
			continue
		case !skillTool:
			part.State, part.Why = session.PartDropped, "the sub-agent has no "+SkillToolName+" tool"
		case one.Hidden:
			part.State, part.Why = session.PartHeld, "the skill is hidden from the listing"
		}
		parts = append(parts, part)
	}
	return parts
}

func unofferedLibrarySkills(library fs.FS, listed []skill.Skill, loaded []PromptPart) []PromptPart {
	var dropped []PromptPart
	for _, file := range librarySkillFiles(library, "*") {
		name := strings.TrimSuffix(path.Base(file), ".md")
		if slices.ContainsFunc(listed, func(one skill.Skill) bool { return one.Name == name }) || slices.ContainsFunc(loaded, func(part PromptPart) bool { return part.Name == name }) {
			continue
		}
		dropped = append(dropped, PromptPart{Concern: ConcernSkills, Name: name, File: "library/" + file, State: session.PartDropped,
			Why: "a library skill reaches only a library sub-agent that names it, and the skill listing reads .tofu, .agents, .claude and ~/.tofu/skills"})
	}
	return dropped
}

type Composed struct {
	Task     rule.Task
	Parts    []PromptPart
	HeldBack []rule.Match
	Unsent   []PromptPart
}

func (c Composed) Recorded() []PromptPart { return slices.Concat(c.Parts, c.Unsent) }

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
	var loaded []PromptPart
	if spec.Agent.Name != "" {
		text, autoloaded, err := autoloadedSkills(spec.Agent, spec.Skills)
		if err != nil {
			return Composed{}, err
		}
		part := agentPart(spec.Agent)
		part.Text += text
		composed.Parts = append(composed.Parts, part)
		loaded, composed.Unsent = autoloaded, slices.Concat(autoloaded, referenceParts(spec.Agent))
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
	withoutTask := rule.IndexReaching(spec.Rules, agentDomain, rule.Task{Role: composed.Task.Role, Language: composed.Task.Language, Frameworks: composed.Task.Frameworks})
	for i, match := range rule.IndexReaching(spec.Rules, agentDomain, composed.Task) {
		if !match.Fires {
			held := spec.Rules[i]
			composed.HeldBack = append(composed.HeldBack, match)
			composed.Unsent = append(composed.Unsent, PromptPart{Concern: held.Concern, RuleID: held.ID, File: filepath.ToSlash(held.File), Why: match.Why, State: session.PartHeld})
			continue
		}
		fired := spec.Rules[i]
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
			Why:     match.Why,
			ByTask:  !withoutTask[i].Fires,
		})
	}
	composed.Unsent = append(composed.Unsent, spec.Dropped...)
	for _, off := range spec.SwitchedOff {
		text := "the person switched this rule off on purpose, so it does not apply here"
		if off.Rule.Override.Reason != "" {
			text += ": " + off.Rule.Override.Reason
		}
		composed.Parts = append(composed.Parts, PromptPart{Concern: off.Base.Concern, RuleID: off.Base.ID, File: filepath.ToSlash(off.Rule.File), Text: text, Off: true})
	}
	order := composedOrder()
	slices.SortStableFunc(composed.Parts, func(a, b PromptPart) int {
		return slices.Index(order, a.Concern) - slices.Index(order, b.Concern)
	})
	skillTool := len(spec.Agent.Tools) == 0 || slices.Contains(spec.Agent.Tools, SkillToolName)
	composed.Unsent = slices.Concat(composed.Unsent, skillParts(spec.Skills, skillTool, loaded), unofferedLibrarySkills(shipped.Files(), spec.Skills, loaded))
	if skillTool {
		if listing := skill.Listing(spec.Skills, spec.WindowTokens); listing != "" {
			composed.Parts = append(composed.Parts, PromptPart{Concern: ConcernSkills, Text: listing})
		}
		return composed, nil
	}
	if len(spec.Skills) == 0 {
		return composed, nil
	}
	text := "you have no " + SkillToolName + " tool and load no skill yourself. the skills loaded for you above: " + cmp.Or(strings.Join(spec.Agent.Skills, ", "), "none")
	if named := skillsNamedIn(spec.Task, spec.Skills, spec.Agent.Skills); len(named) > 0 {
		text += ". your brief names " + strings.Join(named, ", ") + ", which you cannot load: work without it and say so in your report"
	}
	composed.Parts = append(composed.Parts, PromptPart{Concern: ConcernSkills, Text: text})
	return composed, nil
}

func skillsNamedIn(task string, skills []skill.Skill, loaded []string) []string {
	var named []string
	for _, one := range skills {
		word := regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(one.Name) + `($|[^\w-])`)
		if !slices.Contains(loaded, one.Name) && word.MatchString(task) {
			named = append(named, one.Name)
		}
	}
	return named
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
		if part.Concern == rule.ConcernEnvironment || part.State != session.PartFired || !keep(part) {
			continue
		}
		if block.Len() > 0 {
			block.WriteString("\n\n")
		}
		block.WriteString(part.rendered())
	}
	return block.String()
}

func (p PromptPart) rendered() string {
	return fmt.Sprintf("[%s, from %s]\n%s", p.Concern, p.Rule(), p.Text)
}

func TaskNamed(text string) rule.Task {
	task := rule.Task{Text: text}
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, ",;:()[]{}\"'`")
		if strings.Contains(word, "://") {
			continue
		}
		if strings.ContainsAny(word, `/\`) || namedExtension(word) {
			task.Paths = append(task.Paths, filepath.ToSlash(word))
			continue
		}
		if verb := verbNamed(word); verb == rule.VerbPlan || task.Verb == rule.VerbNone {
			task.Verb = verb
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
	if slices.Contains([]rule.Verb{rule.VerbPlan, "ticket", "tickets", "epic", "epics"}, named) {
		return rule.VerbPlan
	}
	if slices.Contains([]rule.Verb{rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite}, named) {
		return named
	}
	return rule.VerbNone
}

const ReferenceToolName = "reference"

type referenceTool struct {
	held map[string]string
}

func (referenceTool) Name() string { return ReferenceToolName }

func (referenceTool) Definition() llm.Tool {
	return llm.Tool{
		Name:        ReferenceToolName,
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
	return Result{Content: text, Command: ReferenceToolName + " " + args.Name, Origin: llm.Origin{Source: llm.SourceReference, Name: args.Name}}, nil
}
