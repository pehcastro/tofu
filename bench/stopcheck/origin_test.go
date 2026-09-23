package stopcheck

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
)

func offlineBattery(t *testing.T, dir string) Battery {
	t.Helper()
	pol, resolution := shippedRule(t)
	return Battery{
		wording:  pol.QuestionsVersion,
		pol:      pol,
		mode:     resolution.Mode,
		reason:   resolution.Reason,
		cache:    ledger.NewCache(filepath.Join(dir, "cache")),
		writer:   ledger.NewWriter(filepath.Join(dir, "log")),
		recorded: ledger.NewReader(filepath.Join(dir, "log")),
	}
}

func firstCorpusTurn(t *testing.T) Turn {
	t.Helper()
	turns, _, err := ReadSessions(corpusDir)
	if err != nil {
		t.Fatalf("ReadSessions %s: %v", corpusDir, err)
	}
	for _, turn := range turns {
		if len(turn.Steps) > 0 {
			return turn
		}
	}
	t.Fatalf("%s holds no turn with a step, so no state can be judged", corpusDir)
	return Turn{}
}

func primeCache(t *testing.T, b Battery, turn Turn) {
	t.Helper()
	built, _, err := state.BuildStopCheck(StateAt(turn, 0))
	if err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}
	request := ledger.Request{State: json.RawMessage(built), Questions: b.pol.Questions, Model: openrouter.Alias, Version: b.wording}
	key, err := b.cache.Key(request)
	if err != nil {
		t.Fatalf("cache.Key: %v", err)
	}
	entry := ledger.Entry{
		RowID:     "2026-09-19-00000000000000000000000000000000",
		Build:     "typesafe/jev-1.13-20260917",
		RequestID: "replayed-from-cache",
		Answers:   stopNowAnswers(b.wording),
	}
	if err := b.cache.Store(key, request, entry); err != nil {
		t.Fatalf("cache.Store: %v", err)
	}
}

func TestARowThisBenchWritesCarriesTheBenchName(t *testing.T) {
	dir := t.TempDir()
	b := offlineBattery(t, dir)
	turn := firstCorpusTurn(t)
	primeCache(t, b, turn)

	out, err := b.decide(context.Background(), turn, 0)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !out.Replayed {
		t.Fatalf("the primed state was judged fresh, and this test must make no call")
	}

	written, present, err := ledger.NewReader(filepath.Join(dir, "log")).ByID(out.RowID)
	if err != nil || !present {
		t.Fatalf("row %s is not readable back: present %v err %v", out.RowID, present, err)
	}
	if written.Bench != BenchName {
		t.Fatalf("row %s came back naming bench %q, want %q, so a count over the ledger cannot tell a measurement from a decision", written.ID, written.Bench, BenchName)
	}
	t.Logf("row %s reads back carrying bench %q, origin %s", written.ID, written.Bench, written.Origin())
}

func TestTheBenchOriginSelectsTheMeasurementsAndNotTheTurns(t *testing.T) {
	dir := t.TempDir()
	b := offlineBattery(t, dir)
	turn := firstCorpusTurn(t)
	primeCache(t, b, turn)

	measured, err := b.decide(context.Background(), turn, 0)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}

	built, builder, err := state.BuildStopCheck(StateAt(turn, 0))
	if err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}
	fromTurn, err := appendRow(b.writer, rowInput{
		state: json.RawMessage(built), stateBuilder: builder, wording: b.wording,
		answers: stopNowAnswers(b.wording), pol: b.pol, mode: b.mode, modeReason: b.reason,
		turnID: turn.ID,
	})
	if err != nil {
		t.Fatalf("appendRow for the turn: %v", err)
	}

	reader := ledger.NewReader(filepath.Join(dir, "log"))
	for _, want := range []struct {
		origin ledger.Origin
		id     string
	}{
		{ledger.OriginBench, measured.RowID},
		{ledger.OriginTurn, fromTurn.ID},
	} {
		var selected []string
		report, err := reader.Each(ledger.Filter{Origin: want.origin}, func(row ledger.Row) error {
			selected = append(selected, row.ID)
			return nil
		})
		if err != nil {
			t.Fatalf("Each %s: %v", want.origin, err)
		}
		if len(selected) != 1 || selected[0] != want.id {
			t.Fatalf("origin %s selected %v out of %d rows, want only %s", want.origin, selected, report.Scanned, want.id)
		}
		t.Logf("origin %s selected %d of %d rows: %s", want.origin, len(selected), report.Scanned, selected[0])
	}
}
