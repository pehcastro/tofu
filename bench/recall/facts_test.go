package recall

import (
	"encoding/json"
	"strings"
	"testing"

	rc "tofu/internal/recall"
)

func sourcesRead(session Session) map[string]int {
	read := make(map[string]int)
	for _, step := range session.Steps {
		for _, call := range step.ToolCalls {
			if call.RenderedBytes > 0 {
				read[callSource(call)] = step.Index
			}
		}
	}
	return read
}

func sheetNames(sheet []string, source string) bool {
	for _, line := range sheet {
		if strings.Contains(line, source) {
			return true
		}
	}
	return false
}

func TestATurnThatNeverForksStillCarriesAFactSheet(t *testing.T) {
	cfg, session := recordedTurn(t)
	result := replay(t, cfg, rc.ShippedBands(), session, armNothing)

	if len(result.Forks) != 0 || len(result.Drops) != 0 {
		t.Fatalf("the turn forked %d times and dropped %d results, so the sheet could have come from either",
			len(result.Forks), len(result.Drops))
	}
	if len(result.Carried.Facts) == 0 {
		t.Fatal("a 44 step turn that never forked ends with an empty fact sheet, so the facts band has no producer on an ordinary turn")
	}
	for _, step := range result.Steps[1:] {
		if step.FactsTokens == 0 {
			t.Fatalf("step %d carried no fact sheet, so distillation is still waiting for something to fire", step.Index)
		}
	}
	read := sourcesRead(session)
	for source, step := range read {
		if !sheetNames(result.Carried.Facts, source) {
			t.Fatalf("the turn read %s at step %d and no line of the %d line sheet names it", source, step, len(result.Carried.Facts))
		}
	}
	t.Logf("a turn that never forks ends with %d fact lines, %d tokens of the %d the band reserves, naming every one of the %d sources it read",
		len(result.Carried.Facts), result.Final.Facts, rc.ShippedBands().Facts, len(read))
}

func TestAFactProducedBeforeACompactionSurvivesIt(t *testing.T) {
	cfg, session := recordedTurn(t)
	store := rc.NewStore(t.TempDir())
	result, err := ReplaySession(store, cfg, measuringBands(), session, armRewrite)
	if err != nil {
		t.Fatalf("replay with the in place rewrite: %v", err)
	}
	if len(result.Drops) == 0 {
		t.Fatalf("nothing was dropped at a %d token target, so no fact had a compaction to survive", measuringBands().Target())
	}

	dropped := make(map[string]rc.Entry)
	for _, entry := range result.Carried.Entries {
		if entry.Handle != "" {
			dropped[entry.Handle] = entry
		}
	}
	for _, drop := range result.Drops {
		entry, stands := dropped[drop.Handle]
		if !stands {
			t.Fatalf("the %s result of step %d was dropped into artifact %s and nothing stands in its place", drop.Tool, drop.Step, drop.Handle)
		}
		_, args, _ := strings.Cut(entry.SupersedeKey, " ")
		source := callSource(SessionCall{Tool: entry.Tool, Args: json.RawMessage(args)})
		if !rc.AlreadyDropped(entry.Text) {
			t.Fatalf("the %s result of step %d is still whole, so it was not compacted away", drop.Tool, drop.Step)
		}
		if !sheetNames(result.Carried.Facts, source) {
			t.Fatalf("the %d byte %s result of step %d was dropped and the sheet the next request carries does not name %s: the fact went with the result",
				drop.Bytes, drop.Tool, drop.Step, source)
		}
	}
	t.Logf("%d results were dropped, every one of them stands as a notice and is named in the %d line fact sheet the next request carries",
		len(result.Drops), len(result.Carried.Facts))
}

func TestTheReplayPutsTheCarryWhereTheLoopPutsIt(t *testing.T) {
	cfg, session := recordedTurn(t)
	bands := measuringBands()
	whole := replay(t, cfg, bands, session, armHandles)
	if len(whole.Forks) == 0 {
		t.Fatalf("no fork at a %d token target, so there is no carry to place", bands.Target())
	}

	forkStep := whole.Forks[0].Step
	upTo := Session{ID: session.ID, Task: session.Task}
	for _, step := range session.Steps {
		upTo.Steps = append(upTo.Steps, step)
		if step.Index == forkStep {
			break
		}
	}
	begun := replay(t, cfg, bands, upTo, armHandles)
	if len(begun.Forks) != 1 {
		t.Fatalf("replaying up to step %d forked %d times, want the one fork that ends the session", forkStep, len(begun.Forks))
	}

	unforked := replay(t, cfg, bands, upTo, armNothing)
	if begun.Carried.Instructions != unforked.Carried.Instructions {
		t.Fatal("the fork changed the instructions, so the carry was appended to the system prompt rather than sent as a message")
	}
	if len(begun.Carried.Entries) != 2 {
		t.Fatalf("the session begun by the fork holds %d messages, and the loop begins one with the task and the carry", len(begun.Carried.Entries))
	}
	task, carry := begun.Carried.Entries[0], begun.Carried.Entries[1]
	if task.Text != session.Task || carry.Tool != "" {
		t.Fatalf("the loop sends the task then the carry as user messages, and the replay sends %q then a %s result", task.Text, carry.Tool)
	}

	built := begun.Forks[0].Carry.Text
	held := strings.Join(append([]string{carry.Text}, begun.Carried.Facts...), "\n")
	for _, line := range strings.Split(built, "\n") {
		said := strings.TrimSpace(line)
		if said != "" && !strings.Contains(held, said) {
			t.Fatalf("the carry the fork built says this and the session it began holds it nowhere:\n%s", said)
		}
	}

	recovered, _, err := rc.Distil(rc.NewStore(t.TempDir()), begun.Carried, 0)
	if err != nil {
		t.Fatalf("distil the session the fork began: %v", err)
	}
	if len(recovered) != len(begun.Carried.Facts) {
		t.Fatalf("the session the fork began holds %d fact lines and reading it back recovers %d", len(begun.Carried.Facts), len(recovered))
	}

	asInstructions := rc.Conversation{Instructions: begun.Carried.Instructions + "\n" + built}
	lost, _, err := rc.Distil(rc.NewStore(t.TempDir()), asInstructions, 0)
	if err != nil {
		t.Fatalf("distil the carry written into the instructions: %v", err)
	}
	if len(lost) != 0 {
		t.Fatalf("the same carry written into the instructions recovers %d fact lines, so the two placements are equivalent and this test proves nothing", len(lost))
	}
	t.Logf("the carry is the %d token message after the task, and the %d line sheet reads back whole from there; written into the instructions instead it reads back as %d lines",
		cfg.Tokens(carry.Text), len(recovered), len(lost))
}
