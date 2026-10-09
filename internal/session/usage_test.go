package session

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestAMemoryModelCallIsSpentByTheMemoryAndLeavesTheLeadsModelAlone(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	store := NewStore(t.TempDir())
	log, err := store.Open(Header{ID: "m", At: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range []Event{
		{Kind: EventTurnStart, Body: []byte(`{"task":"t","wire":"anthropic","spend":"subscription","account":2}`)},
		{Kind: EventRequest, Attempt: 1, Body: []byte(`{"model":"sonnet","prompt_tokens":100,"completion_tokens":10}`)},
		{Kind: EventMemoryRequest, Attempt: 1, Body: []byte(`{"model":"haiku","prompt_tokens":700,"completion_tokens":70,"cost_usd":0.25}`)},
		{Kind: EventRequest, Attempt: 1, Body: []byte(`{"model":"sonnet","prompt_tokens":200,"completion_tokens":20}`)},
	} {
		event.At, event.Turn = now.Add(time.Duration(i-10)*time.Minute), "turn-m"
		if _, err := log.Append(event, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	history, err := store.UsageHistory(UsageDay, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	spent := map[UsageSpender]UsageTotals{}
	for _, row := range history.Spenders {
		spent[row.UsageSpender] = row.UsageTotals
	}
	want := map[UsageSpender]UsageTotals{
		{Session: "m", Role: UsageLead, Wire: "anthropic", Spend: "subscription", Account: 2, Model: "sonnet"}: {TokensIn: 300, TokensOut: 30, Requests: 2},
		{Session: "m", Role: UsageMemory, Model: "haiku"}:                                                      {TokensIn: 700, TokensOut: 70, Requests: 1, CostUSD: 0.25},
	}
	if !reflect.DeepEqual(spent, want) {
		t.Fatalf("spenders\n got %+v\nwant %+v", spent, want)
	}
}

func TestUsageHistoryBucketsEveryRequestOnceByLocalHour(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	now := at(8, 12, 0)
	store := NewStore(t.TempDir())
	record := func(header Header, agents []AgentRun, events ...Event) {
		t.Helper()
		log, err := store.Open(header)
		if err != nil {
			t.Fatal(err)
		}
		if err := log.Edit(func(kept *Header) { kept.Agents = agents }); err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			event.Turn = "turn-" + header.ID
			if _, err := log.Append(event, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	event := func(when time.Time, kind EventKind, agent string, attempt int, body string) Event {
		return Event{At: when, Agent: agent, Attempt: attempt, Kind: kind, Body: []byte(body)}
	}
	started := `{"task":"t","wire":"anthropic","spend":"subscription","account":2}`
	record(Header{ID: "a", At: at(8, 10, 0)}, []AgentRun{{Agent: "research-1", Definition: "research"}},
		event(at(8, 10, 5), EventTurnStart, "", 0, started),
		event(at(8, 10, 5), EventToolResult, "", 0, `{"content":"unsifted"}`),
		event(at(8, 10, 6), EventRequest, "", 1, `{"model":"haiku","prompt_tokens":100,"completion_tokens":10,"cache_read_tokens":1000,"cache_write_tokens":50,"cost_usd":0.5}`),
		event(at(8, 10, 7), EventRequest, "", 2, `{"model":"haiku","prompt_tokens":100,"completion_tokens":10}`),
		event(at(8, 10, 8), EventTurnStart, "research-1", 0, `{"task":"r","wire":"anthropic","spend":"subscription","account":3}`),
		event(at(8, 10, 9), EventRequest, "research-1", 1, `{"model":"sonnet","prompt_tokens":40,"completion_tokens":4}`),
		event(at(8, 10, 10), EventToolResult, "", 0, `{"content":"x","sift_saved_bytes":900}`),
		event(at(8, 10, 11), EventTurnEnd, "research-1", 0, `{"model":"sonnet","wall_clock_ms":3000}`),
		event(at(8, 10, 12), EventTurnEnd, "", 0, `{"model":"haiku","wall_clock_ms":7000}`),
		event(at(8, 11, 0), EventRequest, "", 1, `{"model":"haiku","prompt_tokens":10,"completion_tokens":1}`),
	)
	record(Header{ID: "c", At: at(6, 9, 0)}, nil,
		event(at(6, 9, 1), EventTurnStart, "", 0, started),
		event(at(6, 9, 2), EventRequest, "", 1, `{"model":"haiku","prompt_tokens":999}`),
		event(at(8, 8, 0), EventRequest, "", 1, `{"model":"haiku","prompt_tokens":5,"completion_tokens":5}`),
	)
	record(Header{ID: "f", At: at(8, 7, 0), ForkedInto: "g"}, nil,
		event(at(8, 7, 0), EventTurnEnd, "", 0, `{"wall_clock_ms":1000}`),
		event(at(8, 7, 1), EventTurnStart, "", 0, started),
		event(at(8, 7, 5), EventTurnEnd, "", 0, `{"wall_clock_ms":4000}`),
	)
	record(Header{ID: "g", At: at(8, 7, 5)}, nil, event(at(8, 7, 9), EventTurnEnd, "", 0, `{"wall_clock_ms":9000}`))
	record(Header{ID: "broken", At: at(8, 9, 0)}, nil, event(at(8, 9, 1), EventTurnStart, "", 0, started))
	events, err := os.OpenFile(store.EventsPath("broken"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.WriteString("not json\n"); err != nil {
		t.Fatal(err)
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	history, err := store.UsageHistory(UsageDay, now, []ClassifierCall{
		{At: at(8, 10, 6), Turn: "turn-a", CostUSD: 0.25},
		{At: at(8, 10, 30), Turn: "turn-from-a-check", CostUSD: 0.125},
		{At: at(7, 12, 59), Turn: "turn-a", CostUSD: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Buckets) != 24 || !history.Buckets[0].Start.Equal(at(7, 13, 0)) || !history.Buckets[23].Start.Equal(at(8, 12, 0)) {
		t.Fatalf("%d buckets from %v to %v, want 24 hours from 13:00 yesterday to 12:00 today", len(history.Buckets), history.Buckets[0].Start, history.Buckets[len(history.Buckets)-1].Start)
	}
	wantTotal := UsageTotals{TokensIn: 255, TokensOut: 30, CacheRead: 1000, CacheWrite: 50, Requests: 6, TurnMS: 17000, CostUSD: 0.875, SiftedBytes: 900}
	if history.Total != wantTotal {
		t.Fatalf("total\n got %+v\nwant %+v", history.Total, wantTotal)
	}
	for hour, want := range map[int]UsageTotals{
		18: {TurnMS: 10000},
		19: {TokensIn: 5, TokensOut: 5, Requests: 1},
		21: {TokensIn: 240, TokensOut: 24, CacheRead: 1000, CacheWrite: 50, Requests: 4, TurnMS: 7000, CostUSD: 0.875, SiftedBytes: 900},
		22: {TokensIn: 10, TokensOut: 1, Requests: 1},
	} {
		if got := history.Buckets[hour].UsageTotals; got != want {
			t.Errorf("bucket %d at %v\n got %+v\nwant %+v", hour, history.Buckets[hour].Start, got, want)
		}
	}
	spent := map[UsageSpender]UsageTotals{}
	for _, row := range history.Spenders {
		spent[row.UsageSpender] = row.UsageTotals
	}
	lead := UsageSpender{Session: "a", Role: UsageLead, Wire: "anthropic", Spend: "subscription", Account: 2, Model: "haiku"}
	wantSpent := map[UsageSpender]UsageTotals{
		lead: {TokensIn: 210, TokensOut: 21, CacheRead: 1000, CacheWrite: 50, Requests: 2, TurnMS: 7000, CostUSD: 0.5, SiftedBytes: 900},
		{Session: "a", Role: UsageSubAgent, Agent: "research", Wire: "anthropic", Spend: "subscription", Account: 3, Model: "sonnet"}: {TokensIn: 40, TokensOut: 4, Requests: 1},
		{Session: "a", Role: UsageClassifier}: {Requests: 1, CostUSD: 0.25},
		{Role: UsageClassifier}:               {Requests: 1, CostUSD: 0.125},
		{Session: "c", Role: UsageLead, Wire: "anthropic", Spend: "subscription", Account: 2, Model: "haiku"}: {TokensIn: 5, TokensOut: 5, Requests: 1},
		{Session: "f", Role: UsageLead}: {TurnMS: 1000},
		{Session: "g", Role: UsageLead}: {TurnMS: 9000},
	}
	if !reflect.DeepEqual(spent, wantSpent) {
		t.Fatalf("spenders\n got %+v\nwant %+v", spent, wantSpent)
	}
	if len(history.Skipped) != 1 || history.Skipped[0].ID != "broken" {
		t.Fatalf("skipped %+v, want the session whose events do not parse", history.Skipped)
	}
}
