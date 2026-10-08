package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
)

func TestTheTraceNamesACacheBreakOnlyWhereARequestReadLessThanItsOwnAgentLeftCached(t *testing.T) {
	start := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	var events []session.Event
	var exchanges []session.Exchange
	sent := func(request, agent string, at time.Duration, tools string, messages []string, input, read, write, fiveMinutes, oneHour int) {
		body, _ := json.Marshal(map[string]int{"prompt_tokens": input, "cache_read_tokens": read, "cache_write_tokens": write,
			"cache_write_5m_tokens": fiveMinutes, "cache_write_1h_tokens": oneHour})
		if input+read+write > 0 {
			events = append(events, session.Event{ID: request, Agent: agent, Kind: session.EventRequest, Body: body})
		}
		exchanges = append(exchanges, session.Exchange{Request: request, Agent: agent, At: start.Add(at), Wire: llm.WireAnthropic, Tools: tools, Messages: messages})
	}
	head, brief := []string{"system"}, []string{"system", "brief"}
	sent("lead-1", "", 0, "tools", append(head, "task"), 3, 0, 20000, 0, 20000)
	sent("sub-1", "ts-dev-1", time.Second, "sub-tools", brief, 3, 16000, 6000, 6000, 0)
	sent("sub-2", "ts-dev-1", 2*time.Second, "sub-tools", append(brief, "read", "result"), 3, 21980, 900, 900, 0)
	sent("sub-failed", "ts-dev-1", 3*time.Second, "sub-tools", append(brief, "read", "result", "write"), 0, 0, 0, 0, 0)
	exchanges[len(exchanges)-1].Error = "overloaded"
	sent("sub-3", "ts-dev-1", 4*time.Second, "sub-tools", append(brief, "read", "result", "write", "result"), 3, 22860, 700, 700, 0)
	sent("lead-2", "", 5*time.Second, "tools", []string{"system after memory", "task", "reply"}, 3, 16000, 4500, 0, 4500)
	sent("sub-4", "ts-dev-1", 7*time.Minute, "sub-tools changed", append(brief, "read", "result", "write", "result", "next"), 3, 16000, 8000, 8000, 0)
	sent("old-1", "old", 8*time.Minute, "tools", head, 3, 0, 900, 0, 0)
	sent("old-2", "old", 9*time.Minute, "tools", append(head, "more"), 3, 100, 900, 0, 0)
	sent("tail-1", "tail", 8*time.Minute, "tools", head, 569, 13058, 0, 0, 0)
	sent("tail-2", "tail", 8*time.Minute+3*time.Second, "tools", append(head, "more"), 642, 13058, 0, 0, 0)
	sent("plain-1", "plain", 10*time.Minute, "tools", head, 900, 0, 0, 0, 0)
	sent("plain-2", "plain", 11*time.Minute, "tools", append(head, "more"), 1200, 0, 0, 0, 0)

	cache := cacheTrace(events, exchanges)

	var broken []string
	for _, cut := range cache.Breaks {
		broken = append(broken, cut.Request+" after "+cut.After+": "+cut.Differs)
	}
	want := []string{
		"lead-2 after lead-1: message 0 of 3",
		"sub-4 after sub-3: the tools",
		"old-2 after old-1: nothing sent changed",
	}
	if !slices.Equal(broken, want) {
		t.Fatalf("the breaks are\n%s\nwant\n%s", strings.Join(broken, "\n"), strings.Join(want, "\n"))
	}
	if gap := cache.Breaks[1].After; gap != "sub-3" || cache.Breaks[1].Gap != "6m 56s" || cache.Breaks[1].Cached != 23560 || cache.Breaks[1].Read != 16000 {
		t.Fatalf("the tools break reads %+v, want sub-3 6m56s earlier, 16000 read of the 23560 tokens it left cached", cache.Breaks[1])
	}
	for request, words := range map[string]string{
		"sub-2":      "read 21980 · wrote 900 at 5m",
		"lead-1":     "read 0 · wrote 20000 at 1h",
		"old-1":      "read 0 · wrote 900, lifetime not recorded",
		"plain-1":    "",
		"sub-failed": "",
	} {
		if got := cache.Lifetimes[request]; got != words {
			t.Fatalf("%s reads %q, want %q", request, got, words)
		}
	}
}
