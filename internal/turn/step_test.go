package turn

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestTheStepHookReadsTheSameOccupancyTheForkDecidedOn(t *testing.T) {
	config, _ := longTurnConfig(t)
	sessions := make(chan Row, longTurnSteps)
	config.EndedSession = func(row Row) error {
		sessions <- row
		return nil
	}
	var seen []StepRow
	config.Step = func(step StepRow) { seen = append(seen, step) }

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	close(sessions)
	var recorded []StepRow
	for ended := range sessions {
		recorded = append(recorded, ended.Steps...)
	}
	recorded = append(recorded, row.Steps...)

	if len(seen) != len(recorded) {
		t.Fatalf("the hook saw %d steps and the turn recorded %d", len(seen), len(recorded))
	}

	forks := 0
	var forked StepRow
	for index, step := range seen {
		if step.Index != recorded[index].Index {
			t.Fatalf("the hook saw step %d where the turn recorded step %d", step.Index, recorded[index].Index)
		}
		if (step.Occupancy == nil) != (recorded[index].Occupancy == nil) {
			t.Fatalf("step %d: the hook read %v and the turn recorded %v", step.Index, step.Occupancy, recorded[index].Occupancy)
		}
		if step.Occupancy == nil {
			continue
		}
		if *step.Occupancy != *recorded[index].Occupancy {
			t.Fatalf("step %d: the hook read %+v and the turn recorded %+v", step.Index, *step.Occupancy, *recorded[index].Occupancy)
		}
		if step.Fork == nil {
			continue
		}
		forks, forked = forks+1, step
		if step.Occupancy.Total() != step.Fork.TokensBefore {
			t.Fatalf("step %d forked on %d tokens and the hook read %d", step.Index, step.Fork.TokensBefore, step.Occupancy.Total())
		}
	}
	if forks == 0 {
		t.Fatal("the turn never forked, so no step compares the number read against the number decided on")
	}
	t.Logf("%d steps through the hook, %d of them forks; the hook read %d tokens on step %d and the fork decided on %d against target %d",
		len(seen), forks, forked.Occupancy.Total(), forked.Index, forked.Fork.TokensBefore, forked.Occupancy.Target)
}

func TestNoStepReachesTheHookAfterRunHasReturned(t *testing.T) {
	config, _ := longTurnConfig(t)
	var delivered atomic.Int64
	config.Step = func(StepRow) {
		time.Sleep(50 * time.Millisecond)
		delivered.Add(1)
	}

	row, err := Run(context.Background(), config)
	atReturn := delivered.Load()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	time.Sleep(2 * time.Second)
	if settled := delivered.Load(); settled != atReturn {
		t.Fatalf("%d steps had reached the hook when Run returned and %d had two seconds later", atReturn, settled)
	}
	if atReturn == 0 {
		t.Fatalf("no step reached the hook at all, against %d the turn recorded", len(row.Steps))
	}
	t.Logf("%d steps reached the hook and all of them returned before Run did, against %d the turn recorded",
		atReturn, len(row.Steps))
}

func TestABlockedStepHookHoldsUpOnlyTheEndOfTheTurn(t *testing.T) {
	config, model := longTurnConfig(t)
	blocked := make(chan struct{})
	config.Step = func(StepRow) { <-blocked }

	finished := make(chan Row, 1)
	go func() {
		row, err := Run(context.Background(), config)
		if err != nil {
			t.Error("run: " + err.Error())
		}
		finished <- row
	}()

	if !model.waitForAnAskAfter(longTurnSteps, time.After(20*time.Second)) {
		close(blocked)
		t.Fatalf("the loop stalled on its blocked reader before its %dth model call", longTurnSteps+1)
	}
	select {
	case ended := <-finished:
		close(blocked)
		t.Fatalf("Run returned %s with %d steps while the hook was still blocked on the first one", ended.Outcome, len(ended.Steps))
	case <-time.After(200 * time.Millisecond):
	}

	close(blocked)
	select {
	case ended := <-finished:
		t.Logf("the loop reached its last model call with the hook blocked, Run waited, and released it finished %s with %d steps",
			ended.Outcome, len(ended.Steps))
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never finished after the hook was released")
	}
}
