package turn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/crew"
	"tofu/internal/llm"
)

func bashCall(id, command string) llm.Decision {
	args, err := json.Marshal(bashArgs{Command: command})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "bash", Arguments: args})
}

func TestAStoppedCommandIsAFailedResultRatherThanTheEndOfTheTurn(t *testing.T) {
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	stopped, err := json.Marshal(bashArgs{Command: "sleep 30", TimeoutMS: 300})
	if err != nil {
		t.Fatal(err)
	}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: stopped}),
		bashCall("call-2", "echo the model went on > note.txt"),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(bash)))
	if err != nil {
		t.Fatalf("a stopped command ended the turn: %v", err)
	}

	first := firstToolCall(t, row)
	if !strings.Contains(first.Error, "degraded stopped") {
		t.Fatalf("the stopped command did not reach the model as a failed result: %+v", first)
	}
	went, readErr := os.ReadFile(filepath.Join(root, "note.txt"))
	if readErr != nil || !strings.Contains(string(went), "the model went on") {
		t.Fatalf("the model never got to do anything after the stop: %q %v", went, readErr)
	}
	last := row.Steps[len(row.Steps)-1]
	if last.AssistantText != "done" {
		t.Fatalf("the turn did not finish with the model's own answer: %+v", last)
	}
	showRow(t, "a stopped command inside a finished turn", row)
}

func parentTurnWithAShell(t *testing.T, root string, decisions []llm.Decision) (Config, *SpawnTool) {
	t.Helper()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the write tool: %v", err)
	}
	const parentID = "turn-parent"
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(write, bash),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return parentID },
	}
	spawn := NewSpawnTool(parentID, base, &crew.Roster{})
	parent := base
	parent.Task = "hand the work to a child"
	parent.Tools = NewRegistry(write, bash, spawn)
	return parent, spawn
}

func scratchTreeWithTwoOwners(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"mine", "theirs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "theirs", "notes.txt"), []byte("somebody else's file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAChildShellWritingOutsideItsPathsIsRefusedWithNoGateInPlace(t *testing.T) {
	root := scratchTreeWithTwoOwners(t)
	parent, spawn := parentTurnWithAShell(t, root, []llm.Decision{
		spawnCall("call-1", "stay inside mine/", "mine/**"),
		bashCall("call-2", "echo the child reached outside > theirs/notes.txt"),
		messageDecision(),
		messageDecision(),
	})
	if parent.Gate != nil {
		t.Fatal("this run is the gate-off case and the config carries a gate")
	}

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	kept, err := os.ReadFile(filepath.Join(root, "theirs", "notes.txt"))
	if err != nil {
		t.Fatalf("reading the file the child was told to leave alone: %v", err)
	}
	if string(kept) != "somebody else's file\n" {
		t.Fatalf("the shell reached outside the child's paths and rewrote the file as %q", kept)
	}

	child := spawn.Children()[0]
	refusal := firstToolCall(t, child).Error
	for _, want := range []string{"bash", "theirs/notes.txt", "outside the paths this agent holds", "mine/**"} {
		if !strings.Contains(refusal, want) {
			t.Fatalf("the refusal does not name %q: %q", want, refusal)
		}
	}
	showRow(t, "child refused at the shell", child)
	showRow(t, "parent", row)
}

func TestAChildShellInsideItsOwnPathsStillRuns(t *testing.T) {
	root := scratchTreeWithTwoOwners(t)
	parent, spawn := parentTurnWithAShell(t, root, []llm.Decision{
		spawnCall("call-1", "stay inside mine/", "mine/**"),
		bashCall("call-2", "echo written by the child shell > mine/notes.txt"),
		messageDecision(),
		messageDecision(),
	})

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(root, "mine", "notes.txt"))
	if err != nil {
		t.Fatalf("the child's own shell did not do the work: %v", err)
	}
	if string(written) != "written by the child shell\n" {
		t.Fatalf("the child shell wrote %q", written)
	}
	child := spawn.Children()[0]
	call := firstToolCall(t, child)
	if call.Error != "" || call.ExitCode == nil || *call.ExitCode != 0 {
		t.Fatalf("the command inside the child's own paths was not allowed to run: %+v", call)
	}
	showRow(t, "child allowed at the shell", child)
}

func TestAParentShellIsNotHeldToAnyOwnsBecauseItHoldsTheWholeTree(t *testing.T) {
	root := scratchTreeWithTwoOwners(t)
	parent, _ := parentTurnWithAShell(t, root, []llm.Decision{
		bashCall("call-1", "echo the parent owns the tree > theirs/notes.txt"),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if call := firstToolCall(t, row); call.Error != "" {
		t.Fatalf("the parent's own shell was refused: %+v", call)
	}
	written, err := os.ReadFile(filepath.Join(root, "theirs", "notes.txt"))
	if err != nil || string(written) != "the parent owns the tree\n" {
		t.Fatalf("the parent's shell did not write: %q %v", written, err)
	}
}

func shellSaysAboutItself(t *testing.T, tool *BashTool) (string, string) {
	t.Helper()
	cmd := exec.Command(tool.shell, "-c", "uname -o; pwd")
	cmd.Dir = string(tool.root)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("asking the shell what it is: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the shell answered with something other than a family and a directory: %q", out)
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
}

func TestTheShellDescriptionNamesTheShellAndTheDirectoryAsTheShellSpellsThem(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	family, spelled := shellSaysAboutItself(t, tool)
	description := tool.Definition().Description

	for _, want := range []string{family, spelled} {
		if !strings.Contains(description, want) {
			t.Fatalf("the description does not name %q: %q", want, description)
		}
	}
	if native := string(tool.root); native != spelled && strings.Contains(description, native) {
		t.Fatalf("the description spells the directory the host's way %q rather than the shell's %q: %q", native, spelled, description)
	}
}

func TestTheShellDescriptionIsBuiltFromTheToolAndNotFromALiteral(t *testing.T) {
	var descriptions []string
	for range 2 {
		tool, err := NewBashTool(t.TempDir())
		if err != nil {
			t.Fatalf("building the bash tool: %v", err)
		}
		_, spelled := shellSaysAboutItself(t, tool)
		description := tool.Definition().Description
		if !strings.Contains(description, spelled) {
			t.Fatalf("a tool at %q does not say so: %q", spelled, description)
		}
		descriptions = append(descriptions, description)
	}
	if descriptions[0] == descriptions[1] {
		t.Fatalf("two tools in different directories describe themselves identically: %q", descriptions[0])
	}
}

func TestACommandThatFailsSaysSoWithItsExitCode(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"echo the first half ran; exit 3"}`))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 3 {
		t.Fatalf("the exit code is %v", result.ExitCode)
	}
	if !strings.Contains(result.Content, "the first half ran") {
		t.Fatalf("the output the command did produce is gone: %q", result.Content)
	}
	if !strings.Contains(result.Content, "exited 3") {
		t.Fatalf("the result does not say the command failed or with what code: %q", result.Content)
	}
}
