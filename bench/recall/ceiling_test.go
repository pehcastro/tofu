package recall

import (
	"strconv"
	"strings"
	"testing"

	rc "tofu/internal/agent/context/recall"
	"tofu/internal/konst"
)

const (
	setCeilingTokens    = 20000
	replayedModel       = "claude-opus-5"
	replayedModelWindow = 1000000
)

func budgetAt(t *testing.T, ceiling int) rc.Budget {
	t.Helper()
	if ceiling > 0 {
		t.Setenv(rc.CeilingVariable, strconv.Itoa(ceiling))
	}
	budget, err := rc.BudgetUnder(replayedModel, replayedModelWindow, konst.ContextCeilingTokens)
	if err != nil {
		t.Fatalf("budget for %s: %v", replayedModel, err)
	}
	return budget
}

func oneLineOf(text string, limit int) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len(flat) <= limit {
		return flat
	}
	return flat[:limit] + " ..."
}

func TestWhatTheNewSessionIsLeftHoldingWhenASetCeilingForcesAFork(t *testing.T) {
	cfg, recorded := recordedTurn(t)
	budget := budgetAt(t, setCeilingTokens)
	result := replay(t, cfg, budget.Bands, recorded, armDistilled)

	if len(result.Forks) == 0 {
		t.Fatalf("a %d token target never forked on a turn peaking at %d tokens", budget.Bands.Target(), result.Peak.Total())
	}
	for _, fork := range result.Forks {
		t.Logf("fork at step %2d: %6d tokens down to %5d, the carry is %4d tokens and names %2d of the %2d sources the ended session had, built in %d microseconds",
			fork.Step, fork.TokensBefore, fork.TokensAfter, fork.CarryTokens, fork.SourcesNamed, fork.SourcesKnown, fork.BlockedMicros)
	}
	t.Logf("after %d forks the next session went back to %d sources it already had, %d of them not named in any carry, %d bytes in all",
		len(result.Forks), len(result.Refetches), result.BlindRefetches(), result.RefetchedBytes())
	t.Logf("what the last new session was left holding:\n%s", oneLineOf(result.Carried.Instructions, 1200))
}
