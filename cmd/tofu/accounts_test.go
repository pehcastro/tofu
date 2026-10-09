package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

func TestTheScreenSaysAnAccountMovedAndWhatTheMoveCost(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("pong"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := armOpts(t)
	opts.dir, opts.task, opts.noSubAgents = dir, "read the note", true
	built, err := buildTestRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildTestRunTools: %v", err)
	}
	onFirst := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
		}},
	}}
	onSecond := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the note says pong"},
	}}
	moved := false
	held := turn.Accounts{
		Pick: func(context.Context) (turn.Account, error) {
			return turn.Account{ID: 1, Model: onFirst, Headroom: 0.2, Window: "5h"}, nil
		},
		Next: func(context.Context, turn.Account) (turn.Account, bool, error) {
			if moved {
				return turn.Account{}, false, nil
			}
			moved = true
			return turn.Account{ID: 2, Model: onSecond, Headroom: 0.5, Window: "5h"}, true, nil
		},
	}

	config, _ := mustConfig(t, opts, built, runtime{accounts: held, spend: turn.SpendSubscription})
	var taken []turn.AccountTaken
	config.AccountTaken = func(account turn.AccountTaken) { taken = append(taken, account) }
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	var screen bytes.Buffer
	printRunRow(&screen, row, "stub-model", "5h and 7d")
	said := screen.String()
	if !strings.Contains(said, "moved to account #2") || !strings.Contains(said, "fresh prefix") {
		t.Fatalf("the screen does not say the account moved and what it cost:\n%s", said)
	}
	if row.Account != 2 {
		t.Fatalf("the row says account %d after the move", row.Account)
	}
	want := []turn.AccountTaken{{ID: 1, Reason: turn.AccountPicked}, {ID: 2, From: 1, Reason: turn.AccountMoved}}
	if !slices.Equal(taken, want) {
		t.Fatalf("the turn reported spending %+v, want %+v", taken, want)
	}
	t.Logf("\n%s", said)
}
