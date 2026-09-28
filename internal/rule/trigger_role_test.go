package rule_test

import (
	"strings"
	"testing"

	"tofu/internal/rule"
	"tofu/internal/turn"
)

func TestASubAgentRuleReachesTheSubAgentPromptAndNotTheOrchestrators(t *testing.T) {
	rules, err := rule.LoadDir("../../library")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	const boundaries = "[process_discipline, from the rule sub_agent_boundaries]\nyou are a sub-agent"
	for role, wants := range map[rule.Role]bool{rule.RoleSubAgent: true, rule.RoleOrchestrator: false} {
		composed, err := turn.Compose(turn.ComposeSpec{Task: "add the users route in src/users.ts", Paths: []string{"src/users.ts"},
			Environment: "the working directory is a hono app", ToolGuidance: "read before you edit", Rules: rules, Role: role})
		if err != nil {
			t.Fatalf("Compose as %s: %v", role, err)
		}
		if carries := strings.Contains(composed.Head(), boundaries); carries != wants {
			t.Errorf("the %s prompt carries sub_agent_boundaries = %v, want %v:\n%s", role, carries, wants, composed.Head())
		}
	}
}
