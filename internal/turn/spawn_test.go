package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/settings"
	"tofu/internal/subagent"
	"tofu/library"
)

type namedTool string

func (n namedTool) Name() string { return string(n) }

func (n namedTool) Definition() llm.Tool {
	return llm.Tool{Name: string(n), Parameters: map[string]any{"type": "object"}}
}

func (namedTool) Run(context.Context, json.RawMessage) (Result, error) { return Result{}, nil }

func TestASubAgentIsOfferedWriteAndEditOnlyWhenItsDefinitionNamesThem(t *testing.T) {
	root := t.TempDir()
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	base := []Tool{read, write, namedTool("edit"), namedTool("browser_tabs"), namedTool("browser_observe"), namedTool("browser_act"), namedTool("fetch"), namedTool("glob"), namedTool("search"), namedTool("symbols"), namedTool("bash")}
	var names []string
	for _, tool := range base {
		names = append(names, tool.Name())
	}
	found := subagent.Definitions(subagent.Scan{Library: library.Files(), Tools: append(names, "spawn")})
	for agent, want := range map[string]string{
		"browser":  "browser_tabs browser_observe browser_act",
		"research": "read write fetch glob search symbols bash",
		"ts-dev":   "read write edit glob search symbols bash",
		"":         "read write edit browser_tabs browser_observe browser_act fetch glob search symbols bash spawn message",
	} {
		model := &stubModel{decisions: []llm.Decision{spawnCall("call-spawn", "do the piece", "piece/**"), claimDecision("did the piece"), claimDecision("done")}}
		config := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(base...), Caps: Caps{MaxSteps: 5}, ResultBytesCap: 4096,
			ArtifactDir: t.TempDir(), NewID: func() string { return "turn-orchestrator" }}
		spawn := NewSpawnTool("turn-orchestrator", config, &subagent.Roster{})
		spawn.SubAgents = SubAgents{Defined: found.Definitions}
		orchestrator := config
		orchestrator.Task = "hand the piece to " + agent
		orchestrator.Tools = NewRegistry(spawn)
		if agent != "" {
			model.decisions[0].ToolCalls[0].Arguments = json.RawMessage(`{"agent":"` + agent + `","task":"do the piece","owns":["piece/**"]}`)
		}
		if _, err := Run(context.Background(), orchestrator); err != nil {
			t.Fatal(err)
		}
		var offered []string
		for _, tool := range model.requests[1].Tools {
			if tool.Name != "ask" && tool.Name != "artifact_fetch" {
				offered = append(offered, tool.Name)
			}
		}
		t.Logf("agent %q is offered %v", agent, offered)
		if got := strings.Join(offered, " "); got != want {
			t.Errorf("agent %q is offered %q, want %q", agent, got, want)
		}
	}
}

func TestTheSpawnAtTheSettingRunsAndTheOnePastItIsRefusedNamingIt(t *testing.T) {
	const perTurn = 15
	decisions := make([]llm.Decision, perTurn)
	for i := range decisions {
		decisions[i] = claimDecision("done")
	}
	_, spawn := orchestratorTurn(t, t.TempDir(), decisions)
	spawn.Limits = func() SubAgentLimits { return SubAgentLimits{PerTurn: perTurn, Depth: 2} }
	if described := spawn.Definition().Description; !strings.Contains(described, "At most 15 sub-agents per turn") || !strings.Contains(described, settings.SubAgentsPerTurn) {
		t.Errorf("the model is not told the setting's number and name: %q", described)
	}
	spawnNumber := func(n int) error {
		args, err := json.Marshal(spawnArgs{Task: "piece " + strconv.Itoa(n), Owns: []string{"piece" + strconv.Itoa(n) + "/**"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = spawn.Run(context.Background(), args)
		return err
	}
	for n := 1; n <= perTurn; n++ {
		if err := spawnNumber(n); err != nil {
			t.Fatalf("spawn %d of %d was refused: %v", n, perTurn, err)
		}
	}
	refused := spawnNumber(perTurn + 1)
	if refused == nil || !strings.Contains(refused.Error(), settings.SubAgentsPerTurn) || !strings.Contains(refused.Error(), "15") {
		t.Fatalf("spawn 16 with the setting at 15 was not refused naming the setting: %v", refused)
	}
}
