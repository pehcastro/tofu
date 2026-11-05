package loop

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
)

type typecheckIn struct {
	dir      string
	checkers *checkers.Typecheckers
}

func (typecheckIn) Name() string { return "typecheck" }

func (typecheckIn) Definition() core.Tool {
	return core.Tool{Name: "typecheck", Parameters: map[string]any{"type": "object"}}
}

func (c typecheckIn) Run(ctx context.Context, _ json.RawMessage) (contract.Result, error) {
	return c.checkers.Check(ctx, c.dir)
}

func TestATsDevWhoseLastTypecheckFoundAnErrorIsReopened(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	dir := tsProjectHolding(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true}}`, "package.json": `{"name":"fallback"}`, "bun.lock": "{}",
		"src/users.ts": "export const count: number = \"many\";\n",
	})
	typecheckers := checkers.NewTypecheckers()
	t.Cleanup(typecheckers.Close)
	base := Config{
		Model: &stubModel{decisions: []core.Decision{
			edited("src/users.ts"), called("test", map[string]any{}), called("typecheck", map[string]any{}), claimDecision("done"),
		}},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(scriptedCheck("edit"), scriptedCheck("test"), typecheckIn{dir: dir, checkers: typecheckers}),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagents.Roster{})
	spawn.SubAgents.Defined = []subagents.Definition{{Name: "ts-dev", Language: "typescript", Gate: []string{"typecheck", "test"}, Runs: subagents.RunsInherit, Description: "a fixture"}}
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
