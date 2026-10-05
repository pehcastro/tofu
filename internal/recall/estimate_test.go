package recall_test

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/recall"
)

func conversationOf(texts ...string) recall.Conversation {
	c := recall.Conversation{Instructions: strings.Repeat("s", 4000)}
	for i, text := range texts {
		c.Entries = append(c.Entries, recall.Entry{Step: i + 1, Text: text})
	}
	return c
}

func TestEstimateCountsFromTheLastReportAndOnlyEstimatesWhatCameAfter(t *testing.T) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	schemas := strings.Repeat("{\"name\":\"read\"}", 2000)
	sent := conversationOf(strings.Repeat("a", 9000))
	grown := conversationOf(strings.Repeat("a", 9000), strings.Repeat("b", 6000))
	shrunk := conversationOf("artifact h1 holds it")
	added := cfg.MessageTokens(strings.Repeat("b", 6000))
	reported := 50000

	budget, err := recall.BudgetFor("m1", 0)
	if err != nil {
		t.Fatalf("BudgetFor: %v", err)
	}
	measured := func(c recall.Conversation) recall.Occupancy { return recall.Measure(cfg, budget.Bands, c) }
	unanchored := budget.Sending(cfg, schemas)
	anchored := unanchored.Reported(reported, measured(sent))

	plain := measured(sent).Total()
	if got, want := unanchored.Tokens(cfg, sent), plain+cfg.Tokens(schemas); got != want {
		t.Fatalf("with no report the count is %d, want the estimate %d plus the %d token schemas", got, plain, cfg.Tokens(schemas))
	}
	if got := anchored.Tokens(cfg, sent); got != reported {
		t.Fatalf("the request the report answered counts %d, want exactly the %d reported", got, reported)
	}
	if got := anchored.Tokens(cfg, grown); got != reported+added {
		t.Fatalf("after one more message the count is %d, want %d reported plus %d estimated for the message", got, reported, added)
	}
	if got := anchored.Tokens(cfg, shrunk); got >= reported {
		t.Fatalf("a history shrunk after the report still counts %d, at or over the %d reported", got, reported)
	}
	if got := anchored.Reported(0, measured(grown)).Tokens(cfg, grown); got != unanchored.Tokens(cfg, grown) {
		t.Fatalf("a reply with no usage left the count at %d, want the plain estimate %d", got, unanchored.Tokens(cfg, grown))
	}
	if got, more := anchored.Sending(cfg, schemas+schemas).Tokens(cfg, sent), cfg.Tokens(schemas+schemas)-cfg.Tokens(schemas); got != reported+more {
		t.Fatalf("a tool roster %d tokens larger after the report counts %d, want %d", more, got, reported+more)
	}
	if got, want := unanchored.Reported(1000, measured(sent)).Tokens(cfg, grown), unanchored.Tokens(cfg, grown); got != want {
		t.Fatalf("a report of 1000 under an estimate of %d counts %d, want the larger, the estimate", want, got)
	}
	if anchored.Crossed(cfg, grown) {
		t.Fatalf("%d tokens crossed a %d token target", reported+added, budget.Bands.Target())
	}
	over := unanchored.Reported(budget.Bands.Target(), measured(sent))
	if over.Crossed(cfg, sent) || !over.Crossed(cfg, grown) {
		t.Fatalf("a report at the %d token target must cross only once something is added after it", budget.Bands.Target())
	}

	plainJSON, _ := json.Marshal(budget)
	anchoredJSON, _ := json.Marshal(anchored)
	if string(plainJSON) != string(anchoredJSON) {
		t.Fatalf("the report and the schemas reached the recorded budget:\n%s\nagainst\n%s", anchoredJSON, plainJSON)
	}
}

func TestEstimateForksFromTheModelsWindowAndKeepsTheCeilingWithoutOne(t *testing.T) {
	shipped := recall.ShippedBands().Target()
	usable := 200000 - konst.ContextOutputReserveTokens
	cases := []struct {
		name    string
		window  int
		ceiling string
		target  int
		cap     int
	}{
		{"no window keeps the ceiling", 0, "", shipped, konst.ContextCeilingTokens},
		{"a window under the answer's reserve keeps the ceiling", 1000, "", shipped, konst.ContextCeilingTokens},
		{"a 200k window", 200000, "", usable * konst.ContextForkPercentOfUsable / 100, usable},
		{"a 1M window stops at the ceiling", 1000000, "", konst.ContextCeilingTokens * konst.ContextForkPercentOfUsable / 100, konst.ContextCeilingTokens},
		{"a set ceiling wins over the window", 200000, "20000", recall.BandsOf(20000).Target(), 20000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(recall.CeilingVariable, c.ceiling)
			budget, err := recall.BudgetFor("m1", c.window)
			if err != nil {
				t.Fatalf("BudgetFor: %v", err)
			}
			if budget.CeilingTokens != c.cap || budget.Bands.Target() != c.target {
				t.Fatalf("ceiling %d and target %d, want %d and %d", budget.CeilingTokens, budget.Bands.Target(), c.cap, c.target)
			}
			if c.window > konst.ContextOutputReserveTokens && budget.Bands.Target() >= c.window {
				t.Fatalf("the fork comes at %d, at or past the %d token window", budget.Bands.Target(), c.window)
			}
			t.Logf("%s", budget.Record())
		})
	}
}
