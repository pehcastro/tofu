package host

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"tofu/internal/store/session"
)

func TestAFollowLineWithAnUnknownToolOutcomeIsRefusedByName(t *testing.T) {
	for _, said := range []string{`"passed"`, `""`, `"Skipped"`, `"skipped "`, `3`} {
		var line followLine
		err := json.Unmarshal([]byte(`{"Seq":4,"Event":{"Kind":3,"ID":"c1","Outcome":`+said+`}}`), &line)
		if !errors.As(err, new(unknownOutcome)) || !strings.Contains(err.Error(), said) {
			t.Errorf("a follow line naming the outcome %s read as %+v with %v, want a refusal that names it", said, line.Event, err)
		}
	}
}

func TestEveryToolOutcomeCrossesTheFollowLineAsTheSessionSpellsIt(t *testing.T) {
	for _, recorded := range []string{session.ToolOutcomeRan, session.ToolOutcomeFailed, session.ToolOutcomeAborted, session.ToolOutcomeSkipped} {
		sent, _ := json.Marshal(followLine{Event: &Event{Kind: EventToolResult, ID: "c1", Outcome: ToolOutcome(recorded)}})
		var read followLine
		if err := json.Unmarshal(sent, &read); err != nil || read.Event == nil || string(read.Event.Outcome) != recorded {
			t.Errorf("the outcome %q crossed as %s and read back as %+v with %v", recorded, sent, read.Event, err)
		}
	}
	sent, _ := json.Marshal(followLine{Event: &Event{Kind: EventText, Text: "hello"}})
	var read followLine
	if err := json.Unmarshal(sent, &read); err != nil || strings.Contains(string(sent), "Outcome") {
		t.Errorf("an event with no tool outcome crossed as %s and read with %v, want no outcome named and no refusal", sent, err)
	}
}

func TestAToolCompletedCarriesItsOutcomeAndKeepsFailedForTheDesk(t *testing.T) {
	for outcome, failed := range map[ToolOutcome]bool{ToolRan: false, ToolFailed: true, ToolAborted: false, ToolSkipped: true} {
		out := (&items{}).toolCompleted(Identity{}, Event{Kind: EventToolResult, ID: "c1", Outcome: outcome}, time.Now())
		completed, _ := out[0].msg.Params.(*ToolCompleted)
		wired, _ := json.Marshal(completed)
		if completed == nil || completed.Failed != failed || !strings.Contains(string(wired), `"outcome":"`+string(outcome)+`"`) {
			t.Errorf("a %s call completed as %s, want outcome %q and failed %v", outcome, wired, outcome, failed)
		}
	}
}
