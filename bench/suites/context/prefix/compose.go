package prefix

import (
	"tofu/internal/agent/loop"
	"tofu/internal/agent/prompt"
	rule "tofu/internal/librarian/rules"
	shipped "tofu/library"
)

const RealTask = "write a fix for the flaky test in internal/turn/compose_test.go"

const environmentPlaceholder = "excluded from Composed.Head(), so its content cannot move the measured bytes"

const RealToolGuidance = loop.EveryToolIsRelativeToTheWorkingDirectory + prompt.PreferTheToolOverTheShell + " " + loop.SpawnAddendum

func RealRules() ([]rule.Rule, error) {
	return rule.LoadFS(shipped.Files(), "library")
}

func ComposeSystem(rules []rule.Rule) (string, loop.Composed, error) {
	composed, err := loop.Compose(loop.ComposeSpec{
		Task:         RealTask,
		Environment:  environmentPlaceholder,
		ToolGuidance: RealToolGuidance,
		Rules:        rules,
	})
	if err != nil {
		return "", loop.Composed{}, err
	}
	return composed.Head(), composed, nil
}

func FiredRuleIDs(composed loop.Composed) []string {
	var ids []string
	for _, part := range composed.Parts {
		if part.RuleID != "" {
			ids = append(ids, part.RuleID)
		}
	}
	return ids
}
