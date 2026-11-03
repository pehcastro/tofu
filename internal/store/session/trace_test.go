package session

import (
	"reflect"
	"testing"
	"time"
)

func TestTheTraceListsEveryMemoryModelCallWithItsTokensAndWhyEvenWithoutItsExchange(t *testing.T) {
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	store := NewStore(t.TempDir())
	log, err := store.Open(Header{ID: "m", At: at})
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	for _, event := range []Event{
		{ID: "lead-1", Request: "lead-1", Kind: EventRequest, Body: []byte(`{"model":"sonnet","prompt_tokens":100}`)},
		{ID: "memory-1", Request: "memory-1", Kind: EventMemoryRequest, Body: []byte(`{"model":"haiku","prompt_tokens":700,"completion_tokens":70,"cache_read_tokens":5,"cost_usd":0.25}`)},
		{ID: "memory-2", Request: "memory-2", Agent: "go-dev-1", Kind: EventMemoryRequest, Body: []byte(`{"model":"haiku","prompt_tokens":600,"completion_tokens":60}`)},
	} {
		event.At, event.Turn = at, "turn-m"
		appended, err := log.Append(event, nil)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, appended)
	}
	for _, exchange := range []Exchange{
		{Request: "lead-1", At: at, Why: "the lead's step"},
		{Request: "memory-1", At: at, Why: "memory model: merge these two adjacent lines", Wire: "anthropic", Model: "haiku"},
	} {
		if err := log.Exchanged(exchange); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	traced, err := store.Traced("m", events)
	if err != nil {
		t.Fatal(err)
	}
	want := []TracedMemory{
		{Turn: "turn-m", Request: "memory-1", At: at, Why: "memory model: merge these two adjacent lines", Model: "haiku",
			Usage: Usage{InputTokens: 700, OutputTokens: 70, CacheReadTokens: 5}, CostUSD: 0.25},
		{Agent: "go-dev-1", Turn: "turn-m", Request: "memory-2", At: at, Model: "haiku", Usage: Usage{InputTokens: 600, OutputTokens: 60}},
	}
	if !reflect.DeepEqual(traced.Memory, want) {
		t.Fatalf("the memory calls trace as\n%+v\nwant\n%+v", traced.Memory, want)
	}
}
