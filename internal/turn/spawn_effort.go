package turn

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/boardy"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func (t *SpawnTool) open(definition subagent.Definition, effort llm.Effort) (SubAgentModel, error) {
	switch {
	case t.SubAgents.Open == nil && effort != "":
		return SubAgentModel{}, fmt.Errorf("effort %s: this run opens no model of its own for a sub-agent, so the level would be dropped without a word", effort)
	case t.SubAgents.Open == nil:
		return SubAgentModel{}, nil
	}
	refused := func(err error) (SubAgentModel, error) {
		return SubAgentModel{}, fmt.Errorf("the model for %s did not open: %w", cmp.Or(definition.Name, "the sub-agent"), err)
	}
	if effort != "" && t.SubAgents.Takes != nil {
		takes, err := t.SubAgents.Takes(definition)
		if err != nil {
			return refused(err)
		}
		effort = nearestEffort(effort, takes)
	}
	opened, err := t.SubAgents.Open(definition, effort)
	if err != nil {
		return refused(err)
	}
	return opened, nil
}

func nearestEffort(wanted llm.Effort, takes []llm.Effort) llm.Effort {
	if len(takes) == 0 || slices.Contains(takes, wanted) {
		return wanted
	}
	rank := func(effort llm.Effort) int { return slices.Index(llm.Efforts(), effort) }
	distance := func(effort llm.Effort) int { return max(rank(effort)-rank(wanted), rank(wanted)-rank(effort)) }
	nearest := takes[0]
	for _, effort := range takes[1:] {
		if closer := distance(effort) - distance(nearest); closer < 0 || closer == 0 && rank(effort) > rank(nearest) {
			nearest = effort
		}
	}
	return nearest
}

func raisedTwice(ticket string, thread []boardy.Event, revision int, asked llm.Effort) error {
	rank := func(effort string) int { return slices.Index(llm.Efforts(), llm.Effort(effort)) }
	var earlier []string
	raised := false
	for _, event := range thread {
		if event.Kind != boardy.Spawned || event.Effort == "" || event.Revision != revision {
			continue
		}
		raised = raised || len(earlier) > 0 && rank(event.Effort) > rank(earlier[len(earlier)-1])
		earlier = append(earlier, event.Effort)
	}
	if asked == "" || !raised || rank(string(asked)) <= rank(earlier[len(earlier)-1]) {
		return nil
	}
	return fmt.Errorf("spawn refused: %s already ran at %s under this acceptance, so %s would raise effort a second time, and a second raise does not reach an outcome the first one missed. rewrite the outcome instead: revise the acceptance with tofu boardy set %s --acceptance and --reason, or tell the person what cannot be done, then spawn again",
		ticket, strings.Join(earlier, ", then "), asked, ticket)
}

func effortWords(effort llm.Effort) string {
	if effort == "" {
		return ""
	}
	return " at " + string(effort)
}

func clampedWords(asked llm.Effort, opened SubAgentModel) string {
	if asked == "" || opened.Effort == asked {
		return ""
	}
	return fmt.Sprintf("effort %s is not one %s takes, so it runs at %s. ", asked, opened.Slug, opened.Effort)
}

func (s SubAgents) effortProperty() map[string]any {
	const told = "how hard the sub-agent thinks. low unless it must make design decisions the brief does not settle. left out, it thinks as hard as you do"
	if s.Takes == nil {
		return map[string]any{"type": "string", "enum": llm.Efforts(), "description": told}
	}
	var offered, each []string
	var union []llm.Effort
	for _, definition := range append([]subagent.Definition{{}}, enabledSubAgents(s.Defined)...) {
		takes, err := s.Takes(definition)
		if err != nil {
			continue
		}
		union = append(union, takes...)
		listed := cmp.Or(llm.EffortList(takes), "none, so leave effort out")
		offered = append(offered, listed)
		each = append(each, cmp.Or(definition.Name, "left out it")+" takes "+listed)
	}
	levels := slices.DeleteFunc(llm.Efforts(), func(effort llm.Effort) bool { return !slices.Contains(union, effort) })
	if len(levels) == 0 {
		return nil
	}
	said := told + ". " + strings.Join(each, "; ")
	if slices.IndexFunc(offered, func(listed string) bool { return listed != offered[0] }) < 0 {
		said = told + ". the model takes " + offered[0]
	}
	return map[string]any{"type": "string", "enum": levels, "description": said}
}
