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
		if step.Occupancy.Total() >= step.Fork.TokensBefore {
			t.Fatalf("step %d forked on %d tokens and the hook read %d for the request it sent, which cannot already hold the results the fork measured",
				step.Index, step.Fork.TokensBefore, step.Occupancy.Total())
		}
	}
	if forks == 0 {
		t.Fatal("the turn never forked, so no step compares the number read against the number decided on")
	}
	t.Logf("%d steps through the hook, %d of them forks; the hook read %d tokens sent on step %d and the fork decided on %d against target %d",
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

func TestABlockedStepHookHoldsTheTurnAtTheStepItHolds(t *testing.T) {
	config, model := longTurnConfig(t)
	blocked, held := make(chan struct{}), make(chan struct{})
	var asksWhenSeen []int64
	config.Step = func(StepRow) {
		asks, _ := model.asksAndTheNextWakeUp()
		asksWhenSeen = append(asksWhenSeen, asks)
		if len(asksWhenSeen) == 1 {
			held <- struct{}{}
			<-blocked
		}
	}

	finished := make(chan Row, 1)
	go func() {
		row, err := Run(context.Background(), config)
		if err != nil {
			t.Error("run: " + err.Error())
		}
		finished <- row
	}()

	<-held
	if model.waitForAnAskAfter(1, time.After(longEnoughForTheLoopToAskItsNextQuestion)) {
		close(blocked)
		t.Fatal("the loop asked its second question while the hook still held the first step, so whatever the answer reads can be a step behind the turn")
	}
	select {
	case ended := <-finished:
		close(blocked)
		t.Fatalf("Run returned %s with %d steps while the hook was still blocked on the first one", ended.Outcome, len(ended.Steps))
	default:
	}

	close(blocked)
	var ended Row
	select {
	case ended = <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never finished after the hook was released")
	}

	if len(asksWhenSeen) < 2 {
		t.Fatalf("the hook saw %d steps once it was released, against %d the turn recorded", len(asksWhenSeen), len(ended.Steps))
	}
	for index, asks := range asksWhenSeen {
		if want := int64(index + 1); asks != want {
			t.Fatalf("step %d reached the hook after %d questions, want %d: a step that lands late is a step the next answer cannot read",
				index+1, asks, want)
		}
	}
	t.Logf("the hook held the turn at its first step, and released it finished %s with each of its %d steps seen before the next question",
		ended.Outcome, len(asksWhenSeen))
}
