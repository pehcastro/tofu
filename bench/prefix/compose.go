package prefix

import (
	"tofu/internal/prompt"
	"tofu/internal/rule"
	"tofu/internal/turn"
	shipped "tofu/library"
)

const RealTask = "write a fix for the flaky test in internal/turn/compose_test.go"

const environmentPlaceholder = "excluded from Composed.Head(), so its content cannot move the measured bytes"

const RealToolGuidance = turn.EveryToolIsRelativeToTheWorkingDirectory + prompt.PreferTheToolOverTheShell + " " + turn.SpawnAddendum

func RealRules() ([]rule.Rule, error) {
	return rule.LoadFS(shipped.Files(), "library")
}

func ComposeSystem(rules []rule.Rule) (string, turn.Composed, error) {
	composed, err := turn.Compose(turn.ComposeSpec{
		Task:         RealTask,
		Environment:  environmentPlaceholder,
		ToolGuidance: RealToolGuidance,
		Rules:        rules,
	})
	if err != nil {
		return "", turn.Composed{}, err
	}
	return composed.Head(), composed, nil
}

func FiredRuleIDs(composed turn.Composed) []string {
	var ids []string
	for _, part := range composed.Parts {
		if part.RuleID != "" {
			ids = append(ids, part.RuleID)
		}
	}
	return ids
}
