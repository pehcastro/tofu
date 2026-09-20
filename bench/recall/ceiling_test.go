package recall

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	rc "tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
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
	budget, err := rc.BudgetFor(replayedModel, replayedModelWindow)
	if err != nil {
		t.Fatalf("budget for %s: %v", replayedModel, err)
	}
	return budget
}

func recordRun(t *testing.T, budget rc.Budget, recorded Session, result ReplayResult) *session.Store {
	t.Helper()
	store := session.NewStore(t.TempDir())
	row := turn.Row{
		ID:     recorded.ID,
		Schema: turn.SchemaVersion,
		Task:   recorded.Task,
		Model:  budget.Model,
		Spend:  turn.SpendSubscription,
		Root:   recorded.ID,
	}
	for _, step := range result.Steps {
		stepRow := turn.StepRow{Index: step.Index, PromptTokens: step.InputTokens}
		if len(step.Drops) > 0 {
			stepRow.Compaction = &turn.Compaction{Step: step.Index, TokensBefore: step.InputTokens, Drops: step.Drops}
		}
		row.Steps = append(row.Steps, stepRow)
	}
	header, events, err := row.Record()
	if err != nil {
		t.Fatalf("record the run: %v", err)
	}
	header.ContextCeiling = budget.CeilingTokens
	header.ContextTarget = budget.Bands.Target()
	header.AutoCompaction = budget.Record()
	if err := store.Write(header, events); err != nil {
		t.Fatalf("write the run: %v", err)
	}
	return store
}

func compactionsOf(t *testing.T, store *session.Store, id string) []turn.Compaction {
	t.Helper()
	events, err := store.Body(id)
	if err != nil {
		t.Fatalf("read the body of %s: %v", id, err)
	}
	var compactions []turn.Compaction
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step turn.StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("a step of %s does not parse: %v", id, err)
		}
		if step.Compaction != nil {
			compactions = append(compactions, *step.Compaction)
		}
	}
	return compactions
}

func TestARealTurnAtASetTwentyThousandCeilingCompactsAndTheRecordSaysWhatItDropped(t *testing.T) {
	cfg, recorded := recordedTurn(t)
	budget := budgetAt(t, setCeilingTokens)
	result := replay(t, cfg, budget.Bands, recorded, armRewrite)

	store := recordRun(t, budget, recorded, result)
	header, err := store.Header(recorded.ID)
	if err != nil {
		t.Fatalf("read the header back: %v", err)
	}
	if header.ContextCeiling != setCeilingTokens {
		t.Fatalf("the recorded session says the ceiling was %d, want the %d it was run at", header.ContextCeiling, setCeilingTokens)
	}
	t.Log(header.AutoCompaction)

	compactions := compactionsOf(t, store, recorded.ID)
	if len(compactions) == 0 {
		t.Fatalf("a 44 step turn peaking at %d tokens never compacted against a %d token target",
			result.Peak.Total(), budget.Bands.Target())
	}
	replaced := make(map[string]string)
	for _, entry := range result.Carried.Entries {
		if entry.Handle != "" {
			replaced[entry.Handle] = entry.Text
		}
	}
	dropped := 0
	for _, compaction := range compactions {
		for _, drop := range compaction.Drops {
			dropped++
			notice, kept := replaced[drop.Handle]
			if !kept {
				t.Fatalf("step %d dropped the %s result into artifact %s and nothing stands in its place",
					drop.Step, drop.Tool, drop.Handle)
			}
			t.Logf("step %2d dropped the %-14s result of step %2d, %5d bytes, %s\n    what stands there now: %s",
				compaction.Step, drop.Tool, drop.Step, drop.Bytes, drop.Reason, notice)
		}
	}
	t.Logf("%d compactions dropped %d results, carrying %d tokens at the end against a %d token target",
		len(compactions), dropped, result.Final.Total(), budget.Bands.Target())
	t.Logf("what the %d tokens still are: %s", result.Final.Total(), carriedByKind(cfg, result.Carried))
}

func carriedByKind(cfg rc.Config, c rc.Conversation) string {
	said, notices, whole := 0, 0, 0
	for _, entry := range c.Entries {
		switch {
		case entry.Tool == "":
			said += cfg.Tokens(entry.Text)
		case rc.AlreadyDropped(entry.Text):
			notices += cfg.Tokens(entry.Text)
		default:
			whole += cfg.Tokens(entry.Text)
		}
	}
	return fmt.Sprintf("%d in what the model itself said, which compaction never drops, %d in the notices standing in for dropped results, %d in results still held whole, %d in the instructions",
		said, notices, whole, cfg.Tokens(c.Instructions))
}

func TestTheCarriedContextAtASetCeilingStillAnswersTheTaskItWasWorkingOn(t *testing.T) {
	cfg, recorded := recordedTurn(t)
	budget := budgetAt(t, setCeilingTokens)
	result := replay(t, cfg, budget.Bands, recorded, armRewrite)

	if !strings.Contains(result.Carried.Instructions, recorded.Task[:60]) {
		t.Fatal("the compacted context lost the task, so the session no longer knows what it was asked")
	}
	kept, elided := 0, 0
	for _, entry := range result.Carried.Entries {
		if entry.Tool == "" {
			continue
		}
		if rc.AlreadyDropped(entry.Text) {
			elided++
			if !strings.Contains(entry.Text, "artifact_fetch") {
				t.Fatalf("a dropped result left a notice that does not say how to read it back:\n%s", entry.Text)
			}
			continue
		}
		kept++
	}
	if elided == 0 || kept == 0 {
		t.Fatalf("after compaction the context holds %d results whole and %d as notices, and a working context needs both", kept, elided)
	}
	tail := result.Carried.Entries[len(result.Carried.Entries)-1]
	t.Logf("the compacted context holds %d results whole and %d reachable by handle, and the newest step is still there whole:\n%s",
		kept, elided, oneLineOf(tail.Text, 300))
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

func TestTheSameRunAtASetCeilingAndAtTheModelsOwnDifferOnlyInWhenCompactionFires(t *testing.T) {
	cfg, recorded := recordedTurn(t)
	own := budgetAt(t, 0)
	atOwn := replay(t, cfg, own.Bands, recorded, armRewrite)
	set := budgetAt(t, setCeilingTokens)
	atSet := replay(t, cfg, set.Bands, recorded, armRewrite)

	if len(atOwn.Drops) != 0 {
		t.Fatalf("at the model's own %d token ceiling the turn compacted %d times, so the two runs differ in more than when it fires",
			own.CeilingTokens, len(atOwn.Drops))
	}
	if len(atSet.Drops) == 0 {
		t.Fatalf("at a %d token ceiling the turn never compacted, so there is nothing to compare", set.CeilingTokens)
	}
	if len(atOwn.Steps) != len(atSet.Steps) {
		t.Fatalf("the same turn ran %d steps at the model ceiling and %d at %d, so compaction changed the run itself",
			len(atOwn.Steps), len(atSet.Steps), set.CeilingTokens)
	}
	for i, step := range atOwn.Steps {
		if step.Index != atSet.Steps[i].Index {
			t.Fatalf("step %d of the run is index %d at the model ceiling and %d at %d",
				i, step.Index, atSet.Steps[i].Index, set.CeilingTokens)
		}
	}
	if len(atOwn.Carried.Entries) != len(atSet.Carried.Entries) {
		t.Fatalf("the compacted run ends with %d entries against %d, and compaction replaces an entry rather than removing it",
			len(atSet.Carried.Entries), len(atOwn.Carried.Entries))
	}
	differing := 0
	for i, entry := range atOwn.Carried.Entries {
		if entry.Text == atSet.Carried.Entries[i].Text {
			continue
		}
		differing++
		if !rc.AlreadyDropped(atSet.Carried.Entries[i].Text) {
			t.Fatalf("entry %d differs between the two runs and the difference is not a drop notice:\n%s", i, atSet.Carried.Entries[i].Text)
		}
	}
	t.Logf("at the model's own %d token ceiling: target %d, %d compactions, %d tokens carried at the end",
		own.CeilingTokens, own.Bands.Target(), len(atOwn.Drops), atOwn.Final.Total())
	t.Logf("at a set %d token ceiling:          target %d, %d compactions, %d tokens carried at the end",
		set.CeilingTokens, set.Bands.Target(), len(atSet.Drops), atSet.Final.Total())
	t.Logf("the two runs take the same %d steps and their contexts differ at %d of %d entries, every one of them a drop notice",
		len(atOwn.Steps), differing, len(atOwn.Carried.Entries))
}
