package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type typecheckIn struct {
	dir      string
	checkers *Typecheckers
}

func (typecheckIn) Name() string { return "typecheck" }

func (typecheckIn) Definition() llm.Tool {
	return llm.Tool{Name: "typecheck", Parameters: map[string]any{"type": "object"}}
}

func (c typecheckIn) Run(ctx context.Context, _ json.RawMessage) (Result, error) {
	return c.checkers.Check(ctx, c.dir)
}

func TestATsDevWhoseLastTypecheckFoundAnErrorIsReopened(t *testing.T) {
	dir := tsProject(t, "bun", "bun.lock", map[string]string{"src/users.ts": "export const count: number = \"many\";\n"})
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	base := Config{
		Model: &stubModel{decisions: []llm.Decision{
			edited("src/users.ts"), called("test", map[string]any{}), called("typecheck", map[string]any{}), claimDecision("done"),
		}},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(scriptedCheck("edit"), scriptedCheck("test"), typecheckIn{dir: dir, checkers: checkers}),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	spawn.SubAgents.Defined = []subagent.Definition{{Name: "ts-dev", Language: "typescript", Gate: []string{"typecheck", "test"}, Runs: subagent.RunsInherit, Description: "a fixture"}}
	args, err := json.Marshal(spawnArgs{Task: "change the users route", Owns: []string{"src/**"}, Agent: "ts-dev"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.Run(context.Background(), args); err != nil {
		t.Fatalf("spawn returned an error: %v", err)
	}
	reported(t, spawn)
	reopenedFor(t, spawn.SubAgentRows(), 2, "typecheck failed")
}
