package host

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

func TestSendNowTakesOnlyTheNamedMessageAndKeepsTheRestInOrder(t *testing.T) {
	h, _ := New(Config{Now: time.Now})
	t.Cleanup(h.Close)
	first, second, third := h.Steer("one"), h.Steer("two"), h.Steer("three")
	if h.SendNow("never queued") {
		t.Fatal("send now on an id never queued said it sent something")
	}
	if !h.SendNow(second) {
		t.Fatal("send now on a queued message said nothing was queued")
	}
	type read struct {
		id   string
		step int
	}
	var reads []read
	said := func(event Event) { reads = append(reads, read{event.ID, event.Step}) }
	if taken := h.steering.take(said, 4); !slices.Equal(taken, []string{"two"}) {
		t.Fatalf("the step after send now on two took %q", taken)
	}
	if rest := h.steering.take(said, 5); !slices.Equal(rest, []string{"one\n\nthree"}) {
		t.Fatalf("the step after that took %q, want the other two in order", rest)
	}
	if want := []read{{second, 4}, {first, 5}, {third, 5}}; !slices.Equal(reads, want) {
		t.Fatalf("the read events carry %+v, want the ids Steer answered at the step that read each: %+v", reads, want)
	}
	if h.SendNow("") {
		t.Fatal("esc on an empty queue said it sent something, so the stop it should be would be swallowed")
	}
}

func TestAMessageTheLeadTookOffTheChannelLeavesTheOthersTheirIDs(t *testing.T) {
	h, _ := New(Config{Now: time.Now})
	t.Cleanup(h.Close)
	took, kept := h.Steer("one"), h.Steer("two")
	if text := <-h.steering.ready; text != "one" {
		t.Fatalf("the channel gave %q first", text)
	}
	if id := h.steering.heard(); id != took {
		t.Fatalf("the message the lead took carries id %q, want %q", id, took)
	}
	var read []string
	h.steering.take(func(event Event) { read = append(read, event.ID) }, 1)
	if !slices.Equal(read, []string{kept}) {
		t.Fatalf("the message left in the queue was read as %q, want %q", read, kept)
	}
}

type heldAsk struct{ asked chan struct{} }

func (m heldAsk) Ask(ctx context.Context, _ llm.Request) (llm.Decision, error) {
	m.asked <- struct{}{}
	<-ctx.Done()
	return llm.Decision{}, ctx.Err()
}

func TestSendNowCancelsTheAskInFlightAsSentNowAndAStopStaysAStop(t *testing.T) {
	stops, sendNow := make(chan struct{}, 1), make(chan struct{}, 1)
	model := heldAsk{asked: make(chan struct{}, 1)}
	watch := &watcher{held: rosterHolding(t), now: time.Now, emit: func(Event) {}, stop: &leadStop{}, inner: model}
	quiet := watch.stop.listen(stops, sendNow)
	defer quiet()
	ask := func(signal chan struct{}) error {
		answered := make(chan error, 1)
		go func() {
			_, err := watch.Ask(context.Background(), llm.Request{})
			answered <- err
		}()
		<-model.asked
		signal <- struct{}{}
		select {
		case err := <-answered:
			return err
		case <-time.After(2 * time.Second):
			t.Fatal("the ask in flight was never cancelled")
			return nil
		}
	}
	if err := ask(sendNow); !errors.Is(err, turn.SentNow{}) {
		t.Fatalf("send now ended the ask with %v, want turn.SentNow so the step is asked again", err)
	}
	if err := ask(stops); errors.Is(err, turn.SentNow{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("a lead stop ended the ask with %v, want a cancel the turn ends on", err)
	}
}
