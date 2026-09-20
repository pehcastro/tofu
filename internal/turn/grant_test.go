package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/crew"
	"tofu/internal/llm"
	"tofu/internal/session"
)

func recordedGrants(t *testing.T, store *session.Store, id string) []crew.Question {
	t.Helper()
	events, err := store.Body(id)
	if err != nil {
		t.Fatalf("reading the record of %s: %v", id, err)
	}
	var asked []crew.Question
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("a recorded step of %s is not a step row: %v", id, err)
		}
		asked = append(asked, step.Grants...)
	}
	return asked
}

func childReachingOutsideItsPaths(t *testing.T, root string) (Row, *SpawnTool, *session.Store) {
	t.Helper()
	parent, spawn, store := parentWithAStore(t, root, []llm.Decision{
		spawnCall("call-1", "write the note under mine/", "mine/**"),
		writeCall("call-2", "theirs/note.txt", "the child reached outside its paths"),
		writeCall("call-3", "theirs/note.txt", "and asked for the same path again"),
		claimDecision("theirs/note.txt is not mine to write"),
		claimDecision("the child asked for a grant"),
	})
	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(row.ChildIDs) != 1 {
		t.Fatalf("the parent row names %v children, want one", row.ChildIDs)
	}
	return row, spawn, store
}

func TestAChildWritingOutsideItsPathsAsksOnceAndOnlyOnce(t *testing.T) {
	root := t.TempDir()
	row, _, store := childReachingOutsideItsPaths(t, root)

	asked := recordedGrants(t, store, row.ChildIDs[0])
	if len(asked) != 1 {
		t.Fatalf("the child's record carries %d grant rows, want exactly one for two attempts at the same path: %+v", len(asked), asked)
	}
	if asked[0].Kind != crew.Grant || asked[0].Where != "theirs/note.txt" || asked[0].Ticket != row.ChildIDs[0] {
		t.Fatalf("the recorded row is %+v, want a grant naming the path and the child", asked[0])
	}
	if !strings.Contains(asked[0].Ask, "theirs/note.txt") || asked[0].Default == "" {
		t.Fatalf("the grant does not carry a question and the default taken: %+v", asked[0])
	}
	if _, err := os.Stat(filepath.Join(root, "theirs", "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("the refused write reached the disk anyway (stat err %v)", err)
	}
	t.Logf("the orchestrator reads: %s", asked[0].Ask)
}

func TestAChildThatNeedsAPathItWasNotGrantedWaitsForTheAnswer(t *testing.T) {
	root := t.TempDir()
	_, spawn, _ := childReachingOutsideItsPaths(t, root)

	child := onlyChild(t, spawn)
	if child.State != crew.WaitingAnswer {
		t.Fatalf("the child is %s, want %s: nothing it did answers the question it asked", child.State, crew.WaitingAnswer)
	}
	if !strings.Contains(child.Report, "theirs/note.txt") {
		t.Fatalf("the parent's report does not carry the path the child needs:\n%s", child.Report)
	}
}

func TestAChildInsideItsPathsAsksForNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mine"), 0o750); err != nil {
		t.Fatal(err)
	}
	parent, spawn, store := parentWithAStore(t, root, []llm.Decision{
		spawnCall("call-1", "write the note under mine/", "mine/**"),
		writeCall("call-2", "mine/note.txt", "inside the paths the child holds"),
		claimDecision("I wrote mine/note.txt"),
		claimDecision("the child did the work"),
	})
	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if asked := recordedGrants(t, store, row.ChildIDs[0]); len(asked) != 0 {
		t.Fatalf("a child that stayed inside its paths asked for %+v", asked)
	}
	if child := onlyChild(t, spawn); child.State == crew.WaitingAnswer {
		t.Fatal("a child that asked for nothing is waiting for an answer")
	}
}
