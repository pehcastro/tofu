package turn

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/prompt"
	"tofu/internal/rule"
	"tofu/internal/subagent"
)

const TheFormatContract = prompt.TheFormatContract

type PromptPart struct {
	Concern rule.Concern
	RuleID  string
	Agent   string
	File    string
	Text    string
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
	Agent        subagent.Definition
}

func agentPart(definition subagent.Definition) PromptPart {
	var text strings.Builder
	text.WriteString("you are the " + definition.Name + " sub-agent, and these are your instructions:\n" + definition.Instructions)
	for _, reference := range definition.References {
		text.WriteString("\n\nthe reference " + reference.Name + ", placed here whole from " + reference.Path + ":\n" + reference.Text)
	}
	if len(definition.Cut) > 0 {
		fmt.Fprintf(&text, "\n\nthese references were cut to keep them within %d bytes and are not here: %s", konst.SubAgentReferenceBytes, strings.Join(definition.Cut, ", "))
	}
	return PromptPart{Concern: rule.ConcernIdentity, Agent: definition.Name, File: filepath.ToSlash(definition.Path), Text: text.String()}
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
	for _, owned := range spec.Paths {
		composed.Task.Paths = append(composed.Task.Paths, filepath.ToSlash(owned))
	}
	if spec.Agent.Name != "" {
		composed.Parts = append(composed.Parts, agentPart(spec.Agent))
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
	for i, match := range rule.Index(spec.Rules, composed.Task) {
		if !match.Fires {
			composed.HeldBack = append(composed.HeldBack, match)
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
		})
	}
	order := composedOrder()
	slices.SortStableFunc(composed.Parts, func(a, b PromptPart) int {
		return slices.Index(order, a.Concern) - slices.Index(order, b.Concern)
	})
	return composed, nil
}

func (c Composed) System() string {
	var block strings.Builder
	for _, part := range c.Parts {
		if part.Concern == rule.ConcernEnvironment {
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
