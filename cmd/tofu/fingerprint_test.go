package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/judge/state"
	"tofu/internal/turn"
)

const forcePush = "git push --force origin main"

func rowByID(t *testing.T, id string) ledger.Row {
	t.Helper()
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	row, found, err := ledger.NewReader(dir).ByID(id)
	if err != nil || !found {
		t.Fatalf("reading row %s back: found %v, err %v", id, found, err)
	}
	return row
}

func checkCommand(t *testing.T, policyPath, command string) ledger.Row {
	t.Helper()
	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: askReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	row, err := runCheck(context.Background(), client, policyPath, command)
	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	return row
}

func TestCheckWritesARowCarryingTheFingerprintOfTheCommandItJudged(t *testing.T) {
	policyPath := toolGatePolicyPath(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	row := rowByID(t, checkCommand(t, policyPath, forcePush).ID)

	want := state.FingerprintOf(state.ToolGateInput{
		Agent:      "owner-shell",
		Tool:       "bash",
		Input:      map[string]any{"command": forcePush},
		Cwd:        cwd,
		ProjectDir: cwd,
	})
	if want == "" {
		t.Fatal("the command fingerprints as nothing, so this proves nothing")
	}
	if row.Fingerprint != want {
		t.Fatalf("the row read back carries fingerprint %q, and the command fingerprints as %q", row.Fingerprint, want)
	}
	t.Logf("%s  %s  %s", row.ID, row.Verdict, row.Fingerprint)
}

func gateAndCheckRows(t *testing.T) (gated, checked ledger.Row) {
	t.Helper()
	policyPath := toolGatePolicyPath(t)
	dir, _, _ := gateScratch(t, gateFixtureBuild)
	stubJev(t, 200, middlingRiskAskReply)

	checked = rowByID(t, checkCommand(t, policyPath, forcePush).ID)
	time.Sleep(2 * time.Millisecond)

	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("newToolGate: %v", err)
	}
	decision, err := gate.Decide(t.Context(), turn.GateRequest{
		TurnID: "turn-precedent",
		Task:   "push the branch the owner asked for",
		Tool:   "bash",
		Args:   json.RawMessage(`{"command":"` + forcePush + `"}`),
	})
	if err != nil {
		t.Fatalf("the gate did not decide: %v", err)
	}
	return rowByID(t, decision.ID), checked
}

func TestTheSameCommandCheckedByHandAndRunInATurnFingerprintsTheSame(t *testing.T) {
	gated, checked := gateAndCheckRows(t)
	if gated.Fingerprint == "" || checked.Fingerprint == "" {
		t.Fatalf("the turn's row carries %q and the checked row carries %q", gated.Fingerprint, checked.Fingerprint)
	}
	if gated.Fingerprint != checked.Fingerprint {
		t.Fatalf("the same command fingerprints as %q inside a turn and %q from tofu check", gated.Fingerprint, checked.Fingerprint)
	}
	t.Logf("%s and %s both carry %s", checked.ID, gated.ID, gated.Fingerprint)
}

func TestACheckedDecisionIsAPrecedentForTheSameCommandJudgedLater(t *testing.T) {
	gated, checked := gateAndCheckRows(t)
	var out, errOut bytes.Buffer
	if code := whyVerb([]string{gated.ID}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("tofu why exited %d: %s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, checked.ID) {
		t.Fatalf("tofu why on the turn's decision does not name the checked row %s:\n%s", checked.ID, text)
	}
	if !strings.Contains(text, "the same call") {
		t.Fatalf("tofu why does not read the checked row as the same call:\n%s", text)
	}
	_, shortlist, _ := strings.Cut(text, "  precedent  ")
	t.Logf("precedent  %s", shortlist)
}

func TestAReplayedRowCarriesTheFingerprintOfTheRowItReplays(t *testing.T) {
	t.Chdir(t.TempDir())
	set := battery{SetName: "stub_battery", QuestionsVersion: 1, Kinds: map[string]question.Kind{"approval": question.KindNoul}}
	req := jev.Request{
		State:     map[string]string{"command": forcePush},
		Questions: []jev.Question{{ID: "approval", Kind: jev.QuestionNoul, Instructions: "?", True: "t", False: "f"}},
	}
	answers := []ledger.Answer{{Question: "approval", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.11}}

	original, err := appendRow(req.State, set, rowInput{answers: answers, fingerprint: "bash.5ee1f0a0c0de1234"})
	if err != nil {
		t.Fatalf("writing the row to be replayed: %v", err)
	}
	cacheDir, err := ledger.CacheDir()
	if err != nil {
		t.Fatalf("ledger.CacheDir: %v", err)
	}
	cache := ledger.NewCache(cacheDir)
	cacheReq := ledger.Request{State: req.State, Questions: set.SetName, Model: openrouter.Alias, Version: set.QuestionsVersion}
	key, err := cache.Key(cacheReq)
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}
	if err := cache.Store(key, cacheReq, ledger.Entry{RowID: original.ID, Build: original.Build, RequestID: original.RequestID, Answers: answers}); err != nil {
		t.Fatalf("storing the cache entry: %v", err)
	}

	wire := &stubWire{reply: askReply}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := runJudge(context.Background(), client, req, set, false); err != nil {
		t.Fatalf("runJudge: %v", err)
	}
	if wire.calls != 0 {
		t.Fatalf("the judge asked the wire %d times, so this is not the replay path", wire.calls)
	}

	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	var replay ledger.Row
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{}, func(row ledger.Row) error {
		if row.ReplayOf != "" {
			replay = row
		}
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if replay.ReplayOf != original.ID {
		t.Fatalf("no row replays %s, got replay_of %q", original.ID, replay.ReplayOf)
	}
	if replay.Fingerprint != original.Fingerprint {
		t.Fatalf("the replay carries fingerprint %q and the row it replays carries %q", replay.Fingerprint, original.Fingerprint)
	}
	t.Logf("%s replays %s and both carry %s", replay.ID, original.ID, replay.Fingerprint)
}

func TestARowAtAPointThatDefinesNoFingerprintWritesAndReadsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	set := battery{SetName: "stop_check", QuestionsVersion: 1, Kinds: map[string]question.Kind{"work_remains": question.KindNoul}}
	written, err := appendRow(map[string]string{"task": "write the note"}, set, rowInput{
		turnID:       "turn-no-fingerprint",
		stateBuilder: "stop_check@1",
		answers:      []ledger.Answer{{Question: "work_remains", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.9}},
	})
	if err != nil {
		t.Fatalf("writing a row at a point with no fingerprint: %v", err)
	}
	row := rowByID(t, written.ID)
	if row.Fingerprint != "" {
		t.Fatalf("a point that defines no fingerprint wrote %q", row.Fingerprint)
	}
	if row.Schema != ledger.FingerprintSchema {
		t.Fatalf("the row is schema %d, want %d", row.Schema, ledger.FingerprintSchema)
	}
	t.Logf("%s at %s, schema %d, no fingerprint", row.ID, row.Point, row.Schema)
}
