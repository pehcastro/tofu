package turn

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/shell"
	"tofu/internal/subagent"
)

func TestASubAgentsBackgroundShellIsOneTheLeadReachesByTheNameInItsReport(t *testing.T) {
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := shell.OpenAt(t.TempDir())
	ctx := WithShellRegistry(context.Background(), registry)
	if _, err := bash.Run(ctx, json.RawMessage(`{"command":"echo the lead ran this first"}`)); err != nil {
		t.Fatal(err)
	}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "b", Name: "bash", Arguments: json.RawMessage(`{"command":"sleep 60","background":true}`)}),
		claimDecision("started the sleep in the background"),
	}}
	config := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(bash), Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096,
		ArtifactDir: t.TempDir(), NewID: func() string { return "turn-lead" }}
	spawn := NewSpawnTool("turn-lead", config, &subagent.Roster{})
	if _, err := spawn.Run(ctx, json.RawMessage(`{"task":"start a sleep in the background","owns":["piece/**"]}`)); err != nil {
		t.Fatal(err)
	}
	report := reported(t, spawn)
	name := regexp.MustCompile(`(bash-\d+) \(sleep 60\)`).FindStringSubmatch(report)
	if name == nil {
		t.Fatalf("the report names no background shell:\n%s", report)
	}
	t.Cleanup(func() { _ = registry.Kill(name[1]) })
	if name[1] == "bash-1" {
		t.Errorf("the sub-agent's shell took bash-1, the name the lead's own command already had")
	}
	held, err := registry.Read(name[1])
	if err != nil || held.State != shell.Running || held.Owner != "sub-1" || held.Kept != shell.KeptBackground {
		t.Fatalf("the lead reads %s as %+v (%v), want running, owned by sub-1 and kept as background", name[1], held, err)
	}
}
