package recall_test

import (
	"testing"

	"boji/internal/recall"
)

func replay(t *testing.T, cfg recall.Config, bands recall.Bands, session recall.Session, compacting bool) recall.ReplayResult {
	t.Helper()
	result, err := recall.ReplaySession(recall.NewStore(t.TempDir()), cfg, bands, session, compacting)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return result
}

func TestTheSameRecordedTurnReplaysWithCompactionOnAndOff(t *testing.T) {
	cfg := shippedConfig(t)
	session := loadRecordedTurn(t)

	shipped := recall.ShippedBands()
	off := replay(t, cfg, shipped, session, false)
	on := replay(t, cfg, shipped, session, true)
	t.Logf("shipped bands, target %d: off %d input tokens over %d steps, on %d, %d drops",
		shipped.Target(), off.InputTokens(), len(off.Steps), on.InputTokens(), len(on.Drops))
	if on.InputTokens() != off.InputTokens() {
		t.Fatalf("the turn never reaches the %d target, so the two arms must agree: %d against %d",
			shipped.Target(), on.InputTokens(), off.InputTokens())
	}

	small := recall.Bands{Identity: 2000, Facts: 3000, WorkingSet: 20000, Recent: 7500}
	offSmall := replay(t, cfg, small, session, false)
	onSmall := replay(t, cfg, small, session, true)
	t.Logf("bands scaled to a quarter, target %d: off %d input tokens, on %d, saved %d, %d drops",
		small.Target(), offSmall.InputTokens(), onSmall.InputTokens(),
		offSmall.InputTokens()-onSmall.InputTokens(), len(onSmall.Drops))
	for _, step := range onSmall.Steps {
		if step.Dropped == 0 {
			continue
		}
		t.Logf("step %2d: %6d input tokens with compaction, %6d recorded without, %d dropped",
			step.Index, step.InputTokens, step.RecordedTokens, step.Dropped)
	}
	if onSmall.InputTokens() >= offSmall.InputTokens() {
		t.Fatalf("compaction did not lower the input tokens: %d against %d", onSmall.InputTokens(), offSmall.InputTokens())
	}
	if onSmall.Peak.Total() > small.Target() {
		t.Fatalf("with compaction on, the peak %d still passed the %d target", onSmall.Peak.Total(), small.Target())
	}
}
