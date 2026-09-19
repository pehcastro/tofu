package recall_test

import (
	"strings"
	"testing"

	"boji/internal/recall"
)

func loadRecordedTurn(t *testing.T) recall.Session {
	t.Helper()
	session, err := recall.ReadSession("testdata/recorded-turn.json")
	if err != nil {
		t.Fatalf("read recorded turn: %v", err)
	}
	return session
}

func shippedConfig(t *testing.T) recall.Config {
	t.Helper()
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestOccupancyOfARecordedTurnAgainstTheFourBands(t *testing.T) {
	cfg := shippedConfig(t)
	bands := recall.ShippedBands()
	session := loadRecordedTurn(t)

	result, err := recall.ReplaySession(recall.NewStore(t.TempDir()), cfg, bands, session, false)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	t.Logf("recorded turn %s, %d steps, no compaction\npeak occupancy\n%send of turn\n%s",
		session.ID, len(session.Steps), result.Peak, result.Final)

	if result.Final.Total() == 0 {
		t.Fatal("a 44 step turn measured as empty")
	}
	if result.Final.Identity == 0 {
		t.Fatal("the identity band measured as empty, so the task and the cached prefix were never counted")
	}
}

func TestTheEstimatorTracksTheTokensTheProviderActuallyBilled(t *testing.T) {
	cfg := shippedConfig(t)
	session := loadRecordedTurn(t)

	result, err := recall.ReplaySession(recall.NewStore(t.TempDir()), cfg, recall.ShippedBands(), session, false)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	worst, worstStep := 0.0, 0
	for _, step := range result.Steps {
		if step.RecordedTokens < 200 {
			continue
		}
		off := float64(step.InputTokens-step.RecordedTokens) / float64(step.RecordedTokens)
		if off < 0 {
			off = -off
		}
		if off > worst {
			worst, worstStep = off, step.Index
		}
	}
	t.Logf("worst per step error %.1f%% at step %d", worst*100, worstStep)
	if worst > 0.15 {
		t.Fatalf("the estimator is %.1f%% off the billed tokens at step %d: the budget would be measuring the wrong thing", worst*100, worstStep)
	}
}

func TestTheRecentBandHoldsTheNewestStepWholeEvenWhenItIsOversized(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	bands := recall.Bands{Identity: 10, Facts: 10, WorkingSet: 100, Recent: 10}
	conversation := recall.Conversation{
		Entries: []recall.Entry{
			{Step: 1, Tool: "read", Text: strings.Repeat("a", 500)},
			{Step: 2, Tool: "read", Text: strings.Repeat("b", 500)},
			{Step: 2, Tool: "read", Text: strings.Repeat("c", 500)},
		},
	}

	occupancy := recall.Measure(cfg, bands, conversation)
	if occupancy.Recent != 1000 {
		t.Fatalf("recent band = %d tokens, want both entries of step 2, 1000 tokens", occupancy.Recent)
	}
	if occupancy.WorkingSet != 500 {
		t.Fatalf("working set = %d tokens, want the single step 1 entry, 500 tokens", occupancy.WorkingSet)
	}
}
