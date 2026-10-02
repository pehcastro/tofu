package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type scriptedCheck string

func (s scriptedCheck) Name() string { return string(s) }

func (s scriptedCheck) Definition() llm.Tool {
	return llm.Tool{Name: string(s), Parameters: map[string]any{"type": "object"}}
}

func (s scriptedCheck) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command string `json:"command"`
		Exit    int    `json:"exit"`
		Fail    string `json:"fail"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	result := Result{Content: "ran", Command: args.Command, FailureText: args.Fail}
	if s == "bash" {
		result.ExitCode = &args.Exit
	}
	return result, nil
}

func called(tool string, args map[string]any) llm.Decision {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: tool, Name: tool, Arguments: raw})
}

func edited(path string) llm.Decision { return called("edit", map[string]any{"path": path}) }

func bashed(command string, exit int) llm.Decision {
	return called("bash", map[string]any{"command": command, "exit": exit})
}

func gatedRounds(t *testing.T, definition subagent.Definition, decisions ...llm.Decision) []Row {
	t.Helper()
	root := t.TempDir()
	definition.Runs, definition.Description = subagent.RunsInherit, "a fixture"
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(scriptedCheck("edit"), scriptedCheck("bash"), scriptedCheck("typecheck"), scriptedCheck("test")),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawn := NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	spawn.SubAgents.Defined = []subagent.Definition{definition}
	args, err := json.Marshal(spawnArgs{Task: "change the crate", Owns: []string{"src/**", "README.md", "Cargo.toml"}, Agent: definition.Name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawn.Run(context.Background(), args); err != nil {
		t.Fatalf("spawn returned an error: %v", err)
	}
	reported(t, spawn)
	return spawn.SubAgentRows()
}

var rustDev = subagent.Definition{Name: "rust-dev", Language: "rust", Gate: []string{"cargo clippy", "cargo test"}}

func reopenedFor(t *testing.T, rows []Row, want int, named ...string) {
	t.Helper()
	if len(rows) != want {
		t.Fatalf("%d rounds, want %d", len(rows), want)
	}
	if want == 1 {
		return
	}
	for _, check := range named {
		if !strings.Contains(rows[1].Task, check) {
			t.Fatalf("the reopen does not name %q: %q", check, rows[1].Task)
		}
	}
	t.Log(rows[1].Task)
}

func TestARustDevThatRanOnlyFmtIsReopenedNamingClippyAndTest(t *testing.T) {
	rows := gatedRounds(t, rustDev,
		edited("src/lib.rs"), bashed("cargo fmt", 0), claimDecision("done"),
		bashed("cargo clippy --all-targets -- -D warnings", 0), bashed("cargo test -p parser", 0), claimDecision("done"))
	reopenedFor(t, rows, 2, "cargo clippy did not run", "cargo test did not run")
}

func TestARustDevThatRanClippyAndTestAfterItsLastEditIsAccepted(t *testing.T) {
	rows := gatedRounds(t, rustDev,
		bashed("cargo test", 0), edited("src/lib.rs"), bashed("cargo clippy -p parser", 0), bashed("cd crates/parser && cargo test status", 0), claimDecision("done"))
	reopenedFor(t, rows, 1)
}

func TestARustDevWhoseClippyExits101IsReopenedAndARoundOfClippyAloneThenPasses(t *testing.T) {
	rows := gatedRounds(t, rustDev,
		edited("src/lib.rs"), bashed("cargo clippy", 101), bashed("cargo test", 0), claimDecision("done"),
		bashed("cargo clippy", 0), claimDecision("done"))
	reopenedFor(t, rows, 2, "cargo clippy exited 101")
	if strings.Contains(rows[1].Task, "cargo test did not run") {
		t.Fatalf("cargo test passed after the last edit and is still named: %q", rows[1].Task)
	}
}

func TestAGateRunBeforeTheLastEditDoesNotCount(t *testing.T) {
	rows := gatedRounds(t, rustDev,
		edited("src/lib.rs"), bashed("cargo clippy", 0), bashed("cargo test", 0), edited("src/main.rs"), claimDecision("done"),
		bashed("cargo clippy", 0), bashed("cargo test", 0), claimDecision("done"))
	reopenedFor(t, rows, 2, "cargo clippy did not run", "cargo test did not run")
}

func TestASubAgentWithNoGateOrNoChangeInItsLanguageIsUnaffected(t *testing.T) {
	for name, rows := range map[string][]Row{
		"no gate":                 gatedRounds(t, subagent.Definition{Name: "rust-dev", Language: "rust"}, edited("src/lib.rs"), bashed("cargo fmt", 0), claimDecision("done")),
		"only markdown and toml":  gatedRounds(t, rustDev, edited("README.md"), edited("Cargo.toml"), claimDecision("done")),
		"a refused edit":          gatedRounds(t, rustDev, edited("elsewhere/lib.rs"), claimDecision("done")),
		"no language on the file": gatedRounds(t, subagent.Definition{Name: "rust-dev", Gate: rustDev.Gate}, edited("src/lib.rs"), claimDecision("done")),
	} {
		if len(rows) != 1 {
			t.Errorf("%s: %d rounds, want 1: %q", name, len(rows), rows[len(rows)-1].Task)
		}
	}
}

func TestATsDevGateNamesToolsAndAShellLookalikeDoesNotCount(t *testing.T) {
	tsDev := subagent.Definition{Name: "ts-dev", Language: "typescript", Gate: []string{"typecheck", "test"}}
	rows := gatedRounds(t, tsDev,
		edited("src/users.ts"), bashed("bun test", 0), bashed("test -f src/users.ts", 0), called("typecheck", map[string]any{"fail": "TS2322"}), claimDecision("done"),
		called("typecheck", map[string]any{}), called("test", map[string]any{}), claimDecision("done"))
	reopenedFor(t, rows, 2, "typecheck failed", "test did not run")
}
