package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/session"
)

func runBashAs(t *testing.T, owner, command string, cancelled bool) (Result, error) {
	t.Helper()
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), shellOwnerKey{}, owner))
	defer cancel()
	if cancelled {
		cancel()
	}
	raw, _ := json.Marshal(bashArgs{Command: command})
	return tool.Run(ctx, raw)
}

func TestSubAgentBashGuard(t *testing.T) {
	const subAgent = "sub-agent-row-1"

	if _, err := runBashAs(t, subAgent, "sleep 45; ls", true); err == nil || !strings.Contains(err.Error(), "own_paths_only") {
		t.Errorf("sub-agent sleep 45; ls: want a refusal naming own_paths_only, got %v", err)
	}
	if got, err := runBashAs(t, subAgent, "sleep 1; ls", false); err != nil || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("sub-agent sleep 1; ls: want it to run and exit 0, got %+v, %v", got, err)
	}
	if _, err := runBashAs(t, subAgent, "taskkill //PID 1234 //F", true); err == nil || !strings.Contains(err.Error(), "orchestrator") {
		t.Errorf("sub-agent taskkill: want a refusal leaving servers to the orchestrator, got %v", err)
	}
	if _, err := runBashAs(t, session.AuthorOrchestrator, "sleep 45", true); err != nil && strings.Contains(err.Error(), "own_paths_only") {
		t.Errorf("orchestrator sleep 45: the sub-agent guard refused it: %v", err)
	}
}
