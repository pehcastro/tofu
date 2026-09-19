package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"boji/internal/crew"
	"boji/internal/llm"
)

func spawnCall(id, task string, owns ...string) llm.Decision {
	args, err := json.Marshal(spawnArgs{Task: task, Owns: owns})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "spawn", Arguments: args})
}

func writeCall(id, path, content string) llm.Decision {
	args, err := json.Marshal(writeArgs{Path: path, Content: content})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "write", Arguments: args})
}

func parentTurn(t *testing.T, root string, decisions []llm.Decision) (Config, *SpawnTool) {
	t.Helper()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the write tool: %v", err)
	}
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}
	const parentID = "turn-parent"
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(read, write),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return parentID },
	}
	spawn := NewSpawnTool(parentID, base, &crew.Roster{})
	parent := base
	parent.Task = "hand the work to a child"
	parent.Tools = NewRegistry(read, write, spawn)
	return parent, spawn
}

func firstToolCall(t *testing.T, row Row) ToolCallRow {
	t.Helper()
	for _, step := range row.Steps {
		if len(step.ToolCalls) > 0 {
			return step.ToolCalls[0]
		}
	}
	t.Fatalf("row %s recorded no tool call", row.ID)
	return ToolCallRow{}
}

func showRow(t *testing.T, label string, row Row) {
	t.Helper()
	rendered, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		t.Fatalf("rendering the %s row: %v", label, err)
	}
	t.Logf("%s row:\n%s", label, rendered)
}

func TestSpawnRunsAChildInAScratchTreeAndTheRowsNameEachOther(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mine"), 0o750); err != nil {
		t.Fatal(err)
	}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "write the greeting under mine/", "mine/**"),
		writeCall("call-2", "mine/hello.txt", "written by the child"),
		messageDecision(),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(root, "mine", "hello.txt"))
	if err != nil {
		t.Fatalf("the child did not do the work: %v", err)
	}
	if string(written) != "written by the child" {
		t.Fatalf("the child wrote %q", written)
	}

	children := spawn.Children()
	if len(children) != 1 {
		t.Fatalf("expected one child row, got %d", len(children))
	}
	child := children[0]
	if child.ID != "turn-parent-c1" {
		t.Fatalf("the child row does not name its parent: %q", child.ID)
	}
	if !strings.HasPrefix(child.ID, row.ID) {
		t.Fatalf("child %q is not under parent %q", child.ID, row.ID)
	}
	call := firstToolCall(t, row)
	if call.Command != "spawn "+child.ID {
		t.Fatalf("the parent row does not name the child: %+v", call)
	}
	if len(child.Steps) == 0 || len(child.Steps[0].ToolCalls) != 1 {
		t.Fatalf("the child row does not carry the work it did: %+v", child.Steps)
	}

	showRow(t, "parent", row)
	showRow(t, "child", child)
}

func TestAChildWritingOutsideItsPathsIsRefusedAtTheWrite(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"mine", "theirs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "theirs", "notes.txt"), []byte("somebody else's file"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, spawn := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "stay inside mine/", "mine/**"),
		toolCallDecision(llm.ToolCall{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{"path":"theirs/notes.txt"}`)}),
		writeCall("call-3", "theirs/stolen.txt", "the child reached outside"),
		messageDecision(),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "theirs", "stolen.txt")); !os.IsNotExist(err) {
		t.Fatalf("the write happened and was only reported afterwards: %v", err)
	}
	child := spawn.Children()[0]
	if read := firstToolCall(t, child); read.Tool != "read" || read.Error != "" {
		t.Fatalf("ownership holds writes, not reads, and the read was refused: %+v", read)
	}
	refusal := child.Steps[1].ToolCalls[0].Error
	if !strings.Contains(refusal, "outside the paths this agent holds") {
		t.Fatalf("the refusal does not say what was wrong: %q", refusal)
	}
	if !strings.Contains(refusal, "mine/**") {
		t.Fatalf("the refusal does not name the paths the child holds: %q", refusal)
	}
	if !strings.Contains(firstToolCall(t, row).Command, child.ID) {
		t.Fatalf("the parent row does not name the child that was refused")
	}
	t.Logf("refused: %s", refusal)
}

func TestTwoChildrenCannotHoldOverlappingPaths(t *testing.T) {
	root := t.TempDir()
	parent, _ := parentTurn(t, root, []llm.Decision{
		spawnCall("call-1", "the crew package", "internal/crew/**"),
		messageDecision(),
		spawnCall("call-2", "one file inside the crew package", "internal/crew/owns.go"),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	collision := row.Steps[1].ToolCalls[0].Error
	for _, want := range []string{"turn-parent-c1", "internal/crew/**", "internal/crew/owns.go", "overlap"} {
		if !strings.Contains(collision, want) {
			t.Fatalf("the collision does not name %q: %q", want, collision)
		}
	}
	t.Logf("refused: %s", collision)
}

func TestTheDepthBoundRefusesAndNamesItsLimit(t *testing.T) {
	root := t.TempDir()
	var decisions []llm.Decision
	for level := 1; level <= CrewMaxDepth+1; level++ {
		decisions = append(decisions, spawnCall("call-1", "one level deeper", fmt.Sprintf("level%d/**", level)))
	}
	for range CrewMaxDepth + 1 {
		decisions = append(decisions, messageDecision())
	}
	parent, spawn := parentTurn(t, root, decisions)

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	deepest := spawn.Children()[len(spawn.Children())-1]
	refusal := firstToolCall(t, deepest).Error
	want := fmt.Sprintf("a child at depth %d would pass the crew depth limit of %d", CrewMaxDepth+1, CrewMaxDepth)
	if !strings.Contains(refusal, want) {
		t.Fatalf("the deepest child was not refused with %q: %q", want, refusal)
	}
	t.Logf("refused: %s", refusal)
}

func TestTheBreadthBoundRefusesAndNamesItsLimit(t *testing.T) {
	root := t.TempDir()
	var decisions []llm.Decision
	for child := 1; child <= CrewMaxBreadth; child++ {
		decisions = append(decisions, spawnCall("call-1", "a piece of the work", fmt.Sprintf("part%d/**", child)), messageDecision())
	}
	decisions = append(decisions, spawnCall("call-1", "one child too many", "extra/**"), messageDecision())
	parent, spawn := parentTurn(t, root, decisions)

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if len(spawn.Children()) != CrewMaxBreadth {
		t.Fatalf("expected %d children, got %d", CrewMaxBreadth, len(spawn.Children()))
	}
	refusal := row.Steps[CrewMaxBreadth].ToolCalls[0].Error
	want := fmt.Sprintf("already spawned %d children and the crew breadth limit is %d", CrewMaxBreadth, CrewMaxBreadth)
	if !strings.Contains(refusal, want) {
		t.Fatalf("the extra child was not refused with %q: %q", want, refusal)
	}
	t.Logf("refused: %s", refusal)
}
