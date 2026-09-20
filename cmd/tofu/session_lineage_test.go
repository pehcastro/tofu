package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
)

func writeHeader(t *testing.T, store *session.Store, header session.Header) {
	t.Helper()
	if err := store.Write(header, []session.Event{{Kind: session.EventStep, Body: json.RawMessage(`{"index":1}`)}}); err != nil {
		t.Fatal(err)
	}
}

func TestASpawnedChildReadsAsSpawnedAndAForkedOneAsContinued(t *testing.T) {
	store := sessionProject(t)
	at := time.Now().Add(-time.Hour)
	writeHeader(t, store, session.Header{ID: "turn-parent", At: at, Root: "turn-parent", Outcome: "stopped"})
	writeHeader(t, store, session.Header{
		ID: "turn-parent-c1", At: at, Root: "turn-parent-c1", Parent: "turn-parent", Outcome: "stopped",
	})
	writeHeader(t, store, session.Header{
		ID: "turn-parent-f2", At: at, Root: "turn-parent", Parent: "turn-parent",
		ForkKind: string(turn.ForkContinuation), Outcome: "stopped",
	})

	child, _, code := sessionRun(t, "session", "info", "turn-parent-c1")
	if code != exitOK {
		t.Fatalf("tofu session info on the child exited %d", code)
	}
	if !strings.Contains(child, "spawned by turn-parent") {
		t.Fatalf("a spawned child reads as something else:\n%s", child)
	}
	if strings.Contains(child, "continues turn-parent") {
		t.Fatalf("a spawned child reads as a continuation, which is what a fork is:\n%s", child)
	}

	forked, _, code := sessionRun(t, "session", "info", "turn-parent-f2")
	if code != exitOK {
		t.Fatalf("tofu session info on the fork exited %d", code)
	}
	if !strings.Contains(forked, "continues turn-parent") {
		t.Fatalf("a fork does not read as a continuation:\n%s", forked)
	}
}

func TestSessionInfoCarriesTheCeilingTheTargetAndTheCompactionRecord(t *testing.T) {
	store := sessionProject(t)
	budget, err := recall.BudgetFor("a model with a recorded window", 200000)
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	row := turn.Row{
		ID: "turn-measured", Schema: turn.SchemaVersion, At: time.Now(), Task: "read the repo",
		Root: "turn-measured", Outcome: turn.OutcomeStopped, Budget: budget,
	}
	header, events, err := row.Record()
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.Write(header, events); err != nil {
		t.Fatal(err)
	}

	asJSON, _, code := sessionRun(t, "session", "info", "turn-measured", jsonFlag)
	if code != exitOK {
		t.Fatalf("tofu session info --json exited %d", code)
	}
	var read sessionRow
	if err := json.Unmarshal([]byte(asJSON), &read); err != nil {
		t.Fatal(err)
	}
	if read.ContextCeiling != konst.ContextCeilingTokens {
		t.Fatalf("the record says the ceiling was %d, want the %d tofu operates under whatever the model's window is",
			read.ContextCeiling, konst.ContextCeilingTokens)
	}
	if !strings.Contains(read.AutoCompaction, "200000 token window") {
		t.Fatalf("the record does not say the wall the model itself has: %q", read.AutoCompaction)
	}
	if read.ContextTarget != budget.Bands.Target() || read.ContextTarget == 0 {
		t.Fatalf("the record says the target was %d, want %d", read.ContextTarget, budget.Bands.Target())
	}
	if !strings.HasPrefix(read.AutoCompaction, "on, ") {
		t.Fatalf("the record does not say whether compaction ran on its own: %q", read.AutoCompaction)
	}
	t.Logf("ceiling %d target %d: %s", read.ContextCeiling, read.ContextTarget, read.AutoCompaction)
}
