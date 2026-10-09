package session

import (
	"reflect"
	"testing"
	"time"
)

func TestRuleFiresCountEachPromptThatCarriedTheRuleOnce(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, zone)
	store := NewStore(t.TempDir())
	log, err := store.Open(Header{ID: "s", At: now.AddDate(0, 0, -9)})
	if err != nil {
		t.Fatal(err)
	}
	for _, said := range []struct {
		at   time.Time
		kind EventKind
		body any
	}{
		{now.AddDate(0, 0, -8), EventPrompt, PromptBody{System: "[code_rules, from the rule go-errors]\nwrap errors"}},
		{time.Date(2026, 10, 6, 21, 30, 0, 0, zone), EventPrompt, PromptBody{System: "[code_rules, from the rule go-errors-wrap]\nwrap"}},
		{now.Add(-time.Hour), EventPrompt, PromptBody{System: "[code_rules, from the rule go-errors]\na\n\n[safety, from the rule go-errors-wrap]\nb"}},
		{now.Add(-time.Minute), EventMessage, MessageBody{Role: RoleUser, Content: "[x, from the rule go-errors]\n[y, from the rule go-errors]"}},
		{now.Add(-time.Minute), EventMessage, MessageBody{Role: RoleAssistant, Content: "I read [x, from the rule go-errors] in my prompt"}},
		{now.Add(-time.Minute), EventToolResult, ResultBody{Content: "[x, from the rule go-errors]"}},
	} {
		if _, err := log.Append(Event{At: said.at, Kind: said.kind}, said.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	fires, skipped, err := store.RuleFires(now)
	if err != nil || len(skipped) > 0 {
		t.Fatalf("rule fires: %v, skipped %v", err, skipped)
	}
	want := map[string]RuleFires{
		"go-errors":      {Fires: 3, Week: [daysInAWeek]int{0, 0, 0, 0, 0, 0, 2}},
		"go-errors-wrap": {Fires: 2, Week: [daysInAWeek]int{0, 0, 0, 0, 1, 0, 1}},
	}
	if !reflect.DeepEqual(fires, want) {
		t.Fatalf("fires\n got %+v\nwant %+v", fires, want)
	}
}
