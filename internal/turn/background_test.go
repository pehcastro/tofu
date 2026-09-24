package turn

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/shell"
)

func assertOneRunningSurvivor(t *testing.T, registry *shell.Registry, row Row, command string) {
	t.Helper()
	t.Cleanup(func() {
		list, _ := registry.List()
		for _, entry := range list {
			if entry.State == shell.Running {
				_ = registry.Kill(entry.Name)
			}
		}
	})
	list, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].State != shell.Running {
		t.Fatalf("registry after the turn ended: %+v, want the background process still running", list)
	}
	want := "this turn started a background process still running now that it has ended: " +
		list[0].Name + " (" + command + "); see it in the shells tab, or run `tofu shells kill <name>` to stop it"
	if len(row.Warnings) == 0 || row.Warnings[len(row.Warnings)-1] != want {
		t.Fatalf("row.Warnings = %v, want it to end with %q", row.Warnings, want)
	}
}

func TestABackgroundProcessSurvivesATurnThatEndsNormally(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real process that outlives the call")
	}
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 5", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: args}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(bash))
	ctx := WithShellRegistry(context.Background(), registry)

	row, err := Run(ctx, config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("outcome = %s, want stopped", row.Outcome)
	}
	assertOneRunningSurvivor(t, registry, row, "sleep 5")
}

func TestABackgroundProcessSurvivesACancelledTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real process that outlives the call")
	}
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 5", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelling := &cancellingTool{cancel: cancel, after: 1}
	model := &contextModel{decisions: []llm.Decision{
		toolCallDecision(
			llm.ToolCall{ID: "call-1", Name: "bash", Arguments: args},
			llm.ToolCall{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{}`)},
		),
	}}
	config := baseConfig(t, model, NewRegistry(bash, cancelling))
	runCtx := WithShellRegistry(ctx, registry)

	row, err := Run(runCtx, config)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want a cancellation", err)
	}
	assertOneRunningSurvivor(t, registry, row, "sleep 5")
}

func TestABackgroundProcessSurvivesATurnThatEndsOnAStepCap(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real process that outlives the call")
	}
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 5", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: args}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(bash))
	config.Caps = Caps{MaxSteps: 1}
	ctx := WithShellRegistry(context.Background(), registry)

	row, err := Run(ctx, config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStepCap {
		t.Fatalf("outcome = %s, want the step cap", row.Outcome)
	}
	assertOneRunningSurvivor(t, registry, row, "sleep 5")
}
