package turn

import (
	"context"
	"slices"
	"sync"
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
	var mutex sync.Mutex
	var published []StepRow
	config.Step = func(step StepRow) {
		mutex.Lock()
		defer mutex.Unlock()
		published = append(published, step)
	}

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

	var seen []StepRow
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		mutex.Lock()
		seen = slices.Clone(published)
		mutex.Unlock()
		if len(seen) >= len(recorded) || time.Now().After(deadline) {
			break
		}
	}
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

func TestAStepHookThatNeverReturnsDoesNotHoldTheTurnUp(t *testing.T) {
	config, _ := longTurnConfig(t)
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

	select {
	case ended := <-finished:
		close(blocked)
		if len(ended.Steps) == 0 {
			t.Fatal("the turn finished with no step recorded")
		}
		t.Logf("the turn finished %s with %d steps while the reader was still blocked on the first one",
			ended.Outcome, len(ended.Steps))
	case <-time.After(20 * time.Second):
		close(blocked)
		t.Fatal("the turn never finished: the engine waited on its reader")
	}
}
