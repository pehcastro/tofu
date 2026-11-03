package recall

import (
	"path/filepath"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/konst"
	rc "tofu/internal/recall"
)

var (
	armNothing   = Arm{Name: "nothing"}
	armHandles   = Arm{Name: "fork, handles", Carry: rc.HandleCarry, Signpost: konst.FactSignpostBytes}
	armDistilled = Arm{Name: "fork, distilled", Carry: distilledCarry, Signpost: konst.CarrySignpostBytes}
)

func distilledCarry(store *rc.Store, cfg rc.Config, c rc.Conversation) (rc.Carry, error) {
	return rc.CarryWithFiles(store, cfg, c, rc.WordsAlone, 0)
}

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

func widestWorkingSet(t *testing.T, cfg rc.Config, bands rc.Bands) rc.Occupancy {
	t.Helper()
	walked, err := corpus.WalkSessions(realSessionsDir(t))
	if err != nil {
		t.Fatalf("no recorded sessions to measure the working set band against at the %d ceiling: %v", konst.ContextCeilingTokens, err)
	}
	var widest rc.Occupancy
	widestID := ""
	crossed, under, filled, over := 0, 0, 0, 0
	for _, turn := range walked.Turns {
		session := CorpusSession(turn.RecordedTurn)
		if len(session.Steps) == 0 {
			continue
		}
		peak := replay(t, cfg, bands, session, armNothing).Peak
		if peak.Total() > bands.Target() {
			crossed++
			continue
		}
		under++
		if peak.WorkingSet > 0 {
			filled++
		}
		if peak.WorkingSet > bands.WorkingSet {
			over++
		}
		if peak.WorkingSet > widest.WorkingSet {
			widest, widestID = peak, session.ID
		}
	}
	t.Logf("%d recorded turns replayed at the %d ceiling: %d cross the %d target and would fork, %d stay under it, %d of those put tokens in the working set band and %d past its %d cap; the most was %d tokens, in %s\n%s",
		len(walked.Turns), konst.ContextCeilingTokens, crossed, bands.Target(), under, filled, over, bands.WorkingSet, widest.WorkingSet, widestID, rc.OccupancyTable(widest))
	return widest
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
	widest := widestWorkingSet(t, cfg, bands)
	for _, band := range []struct {
		name     string
		filled   int
		occupied int
		cap      int
	}{
		{"identity", result.Peak.Identity, result.Peak.Identity, bands.Identity},
		{"facts", result.Peak.Facts, result.Peak.Facts, bands.Facts},
		{"working set", widest.WorkingSet, result.Peak.WorkingSet, bands.WorkingSet},
		{"recent", result.Peak.Recent, result.Peak.Recent, bands.Recent},
	} {
		if band.filled == 0 && band.cap != 0 {
			t.Fatalf("the %s band reserves %d tokens and no turn measured ever put a single one in it: either give it a producer or set its cap to zero",
				band.name, band.cap)
		}
		if band.occupied > band.cap {
			t.Fatalf("the %s band peaked at %d tokens against a %d cap, so the shares in konst were not set from this turn and another band is holding the room it needs:\n%s",
				band.name, band.occupied, band.cap, rc.OccupancyTable(result.Peak))
		}
	}
}

func TestTheEstimatorTracksTheTokensTheProviderActuallyBilled(t *testing.T) {
	cfg, session := recordedTurn(t)
	result := replay(t, cfg, rc.ShippedBands(), session, armNothing)

	worst, worstStep, sheet := 0.0, 0, 0
	for _, step := range result.Steps {
		if step.RecordedTokens < 200 {
			continue
		}
		sheet += step.FactsTokens
		asRecorded := step.InputTokens - step.FactsTokens
		off := float64(asRecorded-step.RecordedTokens) / float64(step.RecordedTokens)
		if off < 0 {
			off = -off
		}
		if off > worst {
			worst, worstStep = off, step.Index
		}
	}
	t.Logf("worst per step error %.1f%% at step %d, over a conversation the estimator reads at %d tokens and the fact sheet the run itself never carried at %d",
		worst*100, worstStep, result.InputTokens(), sheet)
	if worst > 0.15 {
		t.Fatalf("the estimator is %.1f%% off the billed tokens at step %d: the budget would be measuring the wrong thing", worst*100, worstStep)
	}
}

func TestTheShippedBandsLeaveTheLargestRecordedTurnAlone(t *testing.T) {
	cfg, session := recordedTurn(t)

	shipped := rc.ShippedBands()
	off := replay(t, cfg, shipped, session, armNothing)
	forked := replay(t, cfg, shipped, session, armHandles)
	t.Logf("shipped bands, target %d: peak %d tokens, nothing %d input tokens over %d steps, fork %d with %d forks",
		shipped.Target(), off.Peak.Total(), off.InputTokens(), len(off.Steps), forked.InputTokens(), len(forked.Forks))
	if forked.InputTokens() != off.InputTokens() {
		t.Fatalf("the turn never reaches the %d target, so both arms must agree: nothing %d, fork %d",
			shipped.Target(), off.InputTokens(), forked.InputTokens())
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

	nothing := replay(t, cfg, bands, session, armNothing)
	handles := replay(t, cfg, bands, session, armHandles)
	distilled := replay(t, cfg, bands, session, armDistilled)
	if len(handles.Forks) == 0 {
		t.Fatalf("at a %d target the recorded turn, peaking at %d tokens, never forked",
			bands.Target(), nothing.Peak.Total())
	}

	t.Logf("\n%s", ArmsTable(bands.Target(), []Measured{
		{armNothing.Name, nothing},
		{armHandles.Name, handles},
		{armDistilled.Name, distilled},
	}))
	for _, fork := range handles.Forks {
		t.Logf("fork at step %d: %d tokens to %d, carry %d tokens, built in %d microseconds",
			fork.Step, fork.TokensBefore, fork.TokensAfter, fork.CarryTokens, fork.BlockedMicros)
	}
	if handles.InputTokens() >= nothing.InputTokens() {
		t.Fatalf("the fork did not lower the context carried: %d against %d", handles.InputTokens(), nothing.InputTokens())
	}
}

func TestWhatTheNextSessionHasToRefetchUnderEachWayOfWritingTheCarry(t *testing.T) {
	cfg, session := recordedTurn(t)
	bands := measuringBands()

	arms := []Arm{armHandles, armDistilled}
	for _, arm := range arms {
		result := replay(t, cfg, bands, session, arm)
		if len(result.Forks) != 1 {
			t.Fatalf("%s forked %d times and this measurement wants exactly one", arm.Name, len(result.Forks))
		}
		fork := result.Forks[0]
		t.Logf("%-20s fork at step %2d, carry %5d tokens, built in %7d microseconds, names %2d of %2d sources known, %d refetches after it of which %d blind, %d bytes",
			arm.Name, fork.Step, fork.CarryTokens, fork.BlockedMicros, fork.SourcesNamed, fork.SourcesKnown,
			len(result.Refetches), result.BlindRefetches(), result.RefetchedBytes())
		for _, refetch := range result.Refetches {
			t.Logf("    step %d went back to %s, %d bytes, named in the carry: %v", refetch.Step, refetch.Source, refetch.Bytes, refetch.Named)
		}
		if fork.SourcesNamed == 0 {
			t.Fatalf("%s names none of the %d sources the ended session had, so the new one cannot reach any of them", arm.Name, fork.SourcesKnown)
		}
	}
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
