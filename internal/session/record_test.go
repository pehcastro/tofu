package session

import (
	"errors"
	"testing"
)

func TestNewEventIDIsAFullUUIDAndNeverRepeats(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewEventID()
		if len(id) != 36 {
			t.Fatalf("id %q is %d characters, want the 36 of a uuid", id, len(id))
		}
		if seen[id] {
			t.Fatalf("id %q was generated twice", id)
		}
		seen[id] = true
	}
}

func TestTheIDOfACallIsTheSameOneWhereverItIsAskedFor(t *testing.T) {
	const turn, call = "turn-18d7430fd0c0c304", "toolu_01abcdef"
	first, second := EventIDFor(turn, call), EventIDFor(turn, call)
	if first != second {
		t.Fatalf("the same call was given %q and %q", first, second)
	}
	if len(first) != 36 {
		t.Fatalf("id %q is %d characters, want the 36 of a uuid", first, len(first))
	}
	if EventIDFor(turn, "toolu_01abcdeg") == first || EventIDFor("turn-other", call) == first {
		t.Fatalf("two different calls were given the same id %q", first)
	}
}

func TestAHashThatMatchesTwoEventsResolvesToNeitherSilently(t *testing.T) {
	one := Event{ID: "11111111-0000-4000-8000-0000a3f9c1de", Kind: EventStep}
	two := Event{ID: "22222222-0000-4000-8000-0000a3f9c1de", Kind: EventOutcome}
	unrelated := Event{ID: "33333333-0000-4000-8000-0000bbbbbbbb", Kind: EventMessage}

	_, err := FindByHash([]Event{one, two, unrelated}, "a3f9c1de")
	if !errors.Is(err, ErrEventHashAmbiguous) {
		t.Fatalf("resolving a colliding hash returned %v, want %v", err, ErrEventHashAmbiguous)
	}

	found, err := FindByHash([]Event{one, two, unrelated}, "#bbbbbb")
	if err != nil || found.ID != unrelated.ID {
		t.Fatalf("resolving the hash the screen draws returned %v, %v, want the one event", found, err)
	}

	if _, err := FindByHash([]Event{one, two, unrelated}, "ffffff"); !errors.Is(err, ErrEventHashNotFound) {
		t.Fatalf("resolving a hash nothing matches returned %v, want %v", err, ErrEventHashNotFound)
	}
}

func TestTheFirstSixCharactersAreNotAHashBecauseTheScreenDrawsTheLastSix(t *testing.T) {
	event := Event{ID: "a3f9c100-0000-4000-8000-0000000000de", Kind: EventStep}
	if _, err := FindByHash([]Event{event}, "a3f9c1"); !errors.Is(err, ErrEventHashNotFound) {
		t.Fatalf("the first six characters resolved an event, and trace.Short draws the last six")
	}
}

func TestASessionRecordedBeforeThisChangeReadsBackWithNoIDAndIsNotRefused(t *testing.T) {
	store := NewStore("testdata")
	const id = "turn-preid-sample"

	header, err := store.Header(id)
	if err != nil {
		t.Fatalf("a pre-change header is refused: %v", err)
	}
	if header.Outcome != "error" {
		t.Fatalf("outcome = %q, want error", header.Outcome)
	}

	events, err := store.Body(id)
	if err != nil {
		t.Fatalf("a pre-change body is refused: %v", err)
	}
	if len(events) != 1 || events[0].Kind != EventOutcome {
		t.Fatalf("events = %+v, want the one outcome line this fixture carries", events)
	}
	if events[0].ID != "" || events[0].Parent != "" || events[0].Author != "" || events[0].Attempt != 0 {
		t.Fatalf("a pre-change event carries %+v, want the new fields empty rather than invented", events[0])
	}
}
