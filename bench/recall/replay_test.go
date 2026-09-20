package recall

import (
	"path/filepath"
	"testing"

	rc "tofu/internal/recall"
)

var (
	armNothing = Arm{Name: "nothing"}
	armRewrite = Arm{Name: "in place rewrite", Rewrite: true}
	armHandles = Arm{Name: "fork, handles", Carry: rc.HandleCarry}
)

func recordedTurn(t *testing.T) (rc.Config, Session) {
	t.Helper()
	cfg, err := rc.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	session, err := ReadSession(filepath.Join("testdata", "recorded-turn.json"))
	if err != nil {
		t.Fatalf("read recorded turn: %v", err)
	}
	return cfg, session
}

func replay(t *testing.T, cfg rc.Config, bands rc.Bands, session Session, arm Arm) ReplayResult {
	t.Helper()
	result, err := ReplaySession(rc.NewStore(t.TempDir()), cfg, bands, session, arm)
	if err != nil {
		t.Fatalf("replay %s: %v", arm.Name, err)
	}
	return result
}

func TestOccupancyOfARecordedTurnAgainstTheFourBands(t *testing.T) {
	cfg, session := recordedTurn(t)
	bands := rc.ShippedBands()

	result := replay(t, cfg, bands, session, armNothing)
	t.Logf("recorded turn %s, %d steps, no compaction\npeak occupancy\n%send of turn\n%s",
		session.ID, len(session.Steps), rc.OccupancyTable(result.Peak), rc.OccupancyTable(result.Final))

	if result.Final.Identity == 0 {
		t.Fatal("the identity band measured as empty, so the task and the cached prefix were never counted")
	}
	for _, band := range []struct {
		name     string
		occupied int
		cap      int
	}{
		{"identity", result.Peak.Identity, bands.Identity},
		{"facts", result.Peak.Facts, bands.Facts},
		{"working set", result.Peak.WorkingSet, bands.WorkingSet},
		{"recent", result.Peak.Recent, bands.Recent},
	} {
		if band.occupied == 0 && band.cap != 0 {
			t.Fatalf("the %s band reserves %d tokens and the only turn measured never put a single one in it: either give it a producer or set its cap to zero",
				band.name, band.cap)
		}
		if band.occupied > band.cap {
			t.Fatalf("the %s band peaked at %d tokens against a %d cap, so the cap was not set from this turn",
				band.name, band.occupied, band.cap)
		}
	}
}

func TestTheEstimatorTracksTheTokensTheProviderActuallyBilled(t *testing.T) {
	cfg, session := recordedTurn(t)
	result := replay(t, cfg, rc.ShippedBands(), session, armNothing)

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

func TestTheShippedBandsLeaveTheLargestRecordedTurnAlone(t *testing.T) {
	cfg, session := recordedTurn(t)

	shipped := rc.ShippedBands()
	off := replay(t, cfg, shipped, session, armNothing)
	on := replay(t, cfg, shipped, session, armRewrite)
	forked := replay(t, cfg, shipped, session, armHandles)
	t.Logf("shipped bands, target %d: peak %d tokens, nothing %d input tokens over %d steps, rewrite %d with %d drops, fork %d with %d forks",
		shipped.Target(), off.Peak.Total(), off.InputTokens(), len(off.Steps), on.InputTokens(), len(on.Drops), forked.InputTokens(), len(forked.Forks))
	if on.InputTokens() != off.InputTokens() || forked.InputTokens() != off.InputTokens() {
		t.Fatalf("the turn never reaches the %d target, so all three arms must agree: nothing %d, rewrite %d, fork %d",
			shipped.Target(), off.InputTokens(), on.InputTokens(), forked.InputTokens())
	}
	if len(forked.Forks) != 0 {
		t.Fatalf("the largest turn ever recorded here forked %d times at the shipped target", len(forked.Forks))
	}
}

func measuringBands() rc.Bands {
	return rc.Bands{Identity: 4000, Facts: 0, WorkingSet: 20000, Recent: 8000}
}

func TestEveryArmOnTheRecordedTurnAtATargetItActuallyCrosses(t *testing.T) {
	cfg, session := recordedTurn(t)
	bands := measuringBands()
	recorded, err := ReadSummaries(filepath.Join("testdata", "fork-summaries.json"))
	if err != nil {
		t.Fatalf("read the recorded summaries: %v", err)
	}

	nothing := replay(t, cfg, bands, session, armNothing)
	rewrite := replay(t, cfg, bands, session, armRewrite)
	handles := replay(t, cfg, bands, session, armHandles)
	summary := replay(t, cfg, bands, session, Arm{Name: "fork, summary", Carry: recorded.Carry()})
	if len(rewrite.Drops) == 0 || len(handles.Forks) == 0 {
		t.Fatalf("at a %d target the recorded turn, peaking at %d tokens, neither dropped nor forked",
			bands.Target(), nothing.Peak.Total())
	}

	t.Logf("\n%s", ArmsTable(bands.Target(), []Measured{
		{armNothing.Name, nothing},
		{armRewrite.Name, rewrite},
		{armHandles.Name, handles},
		{"fork, summary", summary},
	}))
	t.Logf("the summary carried %d tokens against the handle list's %d, and cost %d model call and %d ms of latency to produce",
		cfg.Tokens(recorded.Calls[0].Text), handles.Forks[0].CarryTokens, len(recorded.Calls), recorded.Calls[0].LatencyMS)
	for _, fork := range handles.Forks {
		t.Logf("fork at step %d: %d tokens to %d, carry %d tokens, built in %d microseconds",
			fork.Step, fork.TokensBefore, fork.TokensAfter, fork.CarryTokens, fork.BlockedMicros)
	}

	if rewrite.BilledUnits() <= nothing.BilledUnits() {
		t.Fatalf("the in place rewrite now bills less than leaving the prefix cached, %d against %d: the v4 finding has changed",
			rewrite.BilledUnits(), nothing.BilledUnits())
	}
	if handles.InputTokens() >= nothing.InputTokens() {
		t.Fatalf("the fork did not lower the context carried: %d against %d", handles.InputTokens(), nothing.InputTokens())
	}
	if handles.BilledUnits() >= rewrite.BilledUnits() {
		t.Fatalf("the fork bills %d against the rewrite's %d: the mechanism v5 asks for is no better than the one it replaces",
			handles.BilledUnits(), rewrite.BilledUnits())
	}
	t.Logf("the fork bills %d against the rewrite's %d, %.1f%% less",
		handles.BilledUnits(), rewrite.BilledUnits(),
		100*float64(rewrite.BilledUnits()-handles.BilledUnits())/float64(rewrite.BilledUnits()))
}

func TestTheLengthAtWhichAForkStartsBeatingDoingNothingAtTheShippedTarget(t *testing.T) {
	cfg, session := recordedTurn(t)
	bands := rc.ShippedBands()

	won := 0
	for _, steps := range []int{44, 48, 52, 56, 60, 70, 88, 132, 176} {
		longer := Extend(session, steps)
		nothing := replay(t, cfg, bands, longer, armNothing)
		handles := replay(t, cfg, bands, longer, armHandles)
		t.Logf("%3d steps: nothing bills %10d, fork bills %10d over %2d forks, fork is %+.1f%%",
			len(longer.Steps), nothing.BilledUnits(), handles.BilledUnits(), len(handles.Forks),
			100*float64(handles.BilledUnits()-nothing.BilledUnits())/float64(nothing.BilledUnits()))
		if won == 0 && handles.BilledUnits() < nothing.BilledUnits() {
			won = len(longer.Steps)
		}
	}
	if won == 0 {
		t.Log("the fork never bills less than doing nothing, at any length measured here")
		return
	}
	t.Logf("the fork starts billing less than doing nothing at %d steps", won)
}
