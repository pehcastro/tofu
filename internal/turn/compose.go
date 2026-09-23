package turn

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/rule"
)

const TheFormatContract = "finish by saying what changed, naming every file you wrote, " +
	"and pasting the real output of every command you ran. " +
	"a command you did not run is not evidence, and a claim with no command behind it is one the next reader has to redo. " +
	"say what you could not verify and why in the same answer rather than leaving it out."

type PromptPart struct {
	Concern rule.Concern
	RuleID  string
	File    string
	Text    string
}

func (p PromptPart) Rule() string {
	if p.RuleID == "" {
		return "tofu itself"
	}
	return "the rule " + p.RuleID
}

func (p PromptPart) From() string {
	if p.RuleID == "" {
		return "tofu itself"
	}
	return p.Rule() + " in " + p.File
}

type ComposeSpec struct {
	Task         string
	Environment  string
	ToolGuidance string
	Rules        []rule.Rule
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
