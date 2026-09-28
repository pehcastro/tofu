package jevloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"tofu/internal/browser"
)

type fakeTab struct {
	page      browser.Page
	changes   bool
	staleActs int
	staleOnce bool
	acts      []browser.Action
}

func (f *fakeTab) browser() Browser {
	return Browser{
		Snapshot: func(context.Context) (browser.Page, error) { return f.page, nil },
		Fresh: func(context.Context, browser.Page) (bool, error) {
			if f.staleOnce {
				f.staleOnce = false
				return false, nil
			}
			return true, nil
		},
		Act: func(_ context.Context, _ browser.Page, action browser.Action) (bool, error) {
			if f.staleActs > 0 {
				f.staleActs--
				return false, nil
			}
			f.acts = append(f.acts, action)
			if f.changes {
				f.page.Fingerprint = fmt.Sprint("hotel-", len(f.acts))
			}
			return true, nil
		},
	}
}

func fixedText(context.Context, string, browser.Page, browser.Element) (string, error) {
	return "Lisbon", nil
}

func recordedLoop(t *testing.T, tab *fakeTab, wire *recordedWire, actions int) Loop {
	t.Helper()
	tab.page = hotelPage(t)
	return Loop{Browser: tab.browser(), Choose: recordedChooser(t, wire).Choose, Write: fixedText, Actions: actions}
}

func always(action browser.Action) Chooser {
	return func(context.Context, string, browser.Page, []Step) (Choice, error) {
		return Choice{Action: action}, nil
	}
}

func assertBlocked(t *testing.T, result Result, reason string) {
	t.Helper()
	if result.Status != StatusBlocked || !strings.Contains(result.Reason, reason) {
		t.Fatalf("stopped %v with %q, want blocked with %q", result.Status, result.Reason, reason)
	}
}

func TestThreeActionsThatChangedNothingStopAsBlocked(t *testing.T) {
	tab := &fakeTab{}
	result := recordedLoop(t, tab, &recordedWire{answer: readFixture(t, "hotel_answer.json")}, 60).Run(context.Background(), "goal")
	assertBlocked(t, result, "changed nothing")
	if len(tab.acts) != 3 || len(result.Steps) != 3 {
		t.Fatalf("ran %d actions and recorded %d steps, want 3 and 3", len(tab.acts), len(result.Steps))
	}
}

func TestWaitThatChangedNothingIsNotCountedAsNoChange(t *testing.T) {
	tab := &fakeTab{page: browser.Page{Fingerprint: "still"}}
	result := Loop{Browser: tab.browser(), Choose: always(browser.Action{Op: browser.OpWait}), Actions: 5}.Run(context.Background(), "goal")
	assertBlocked(t, result, "action budget")
	if len(tab.acts) != 5 {
		t.Fatalf("ran %d waits, want 5", len(tab.acts))
	}
}

func TestTheActionBudgetStopsAsBlocked(t *testing.T) {
	tab := &fakeTab{changes: true}
	result := recordedLoop(t, tab, &recordedWire{answer: readFixture(t, "hotel_answer.json")}, 4).Run(context.Background(), "goal")
	assertBlocked(t, result, "action budget")
	if len(tab.acts) != 4 {
		t.Fatalf("ran %d actions under a budget of 4", len(tab.acts))
	}
}

func TestAJevErrorRunsNoAction(t *testing.T) {
	tab := &fakeTab{changes: true}
	wire := &recordedWire{fail: errors.New("connection refused")}
	result := recordedLoop(t, tab, wire, 60).Run(context.Background(), "goal")
	assertBlocked(t, result, "connection refused")
	if len(tab.acts) != 0 || len(wire.posted) != 1 {
		t.Fatalf("ran %d actions after %d Jev calls, want none after one", len(tab.acts), len(wire.posted))
	}
}

func TestAStaleElementRetriesOnceWithAFreshDecision(t *testing.T) {
	tab := &fakeTab{changes: true, staleActs: 1}
	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	result := recordedLoop(t, tab, wire, 2).Run(context.Background(), "goal")
	assertBlocked(t, result, "action budget")
	if len(tab.acts) != 2 || len(wire.posted) != 4 || len(result.Steps) != 2 || result.Steps[0].Stale {
		t.Fatalf("ran %d actions over %d decisions into %d steps; want 2 over 4 into 2, none stale", len(tab.acts), len(wire.posted), len(result.Steps))
	}
}

func TestAStaleElementTwiceIsRecordedAsAFailedStep(t *testing.T) {
	tab := &fakeTab{staleActs: 2}
	result := recordedLoop(t, tab, &recordedWire{answer: readFixture(t, "hotel_answer.json")}, 60).Run(context.Background(), "goal")
	assertBlocked(t, result, "changed nothing")
	if len(result.Steps) != 4 || !result.Steps[0].Stale || result.Steps[1].Stale || len(tab.acts) != 3 {
		t.Fatalf("%d steps with %d actions, want one stale step then three that ran", len(result.Steps), len(tab.acts))
	}
}

func TestAPageThatIsAlwaysStaleStopsAtTheDecisionBudget(t *testing.T) {
	tab := &fakeTab{staleActs: 1000, changes: true}
	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	result := recordedLoop(t, tab, wire, 3).Run(context.Background(), "goal")
	assertBlocked(t, result, "decision budget")
	if len(tab.acts) != 0 || len(wire.posted) != 6 {
		t.Fatalf("ran %d actions over %d decisions, want none over 6", len(tab.acts), len(wire.posted))
	}
}

func TestDoneOnAStalePageIsDecidedAgain(t *testing.T) {
	tab := &fakeTab{page: browser.Page{Fingerprint: "p"}, staleOnce: true}
	result := Loop{Browser: tab.browser(), Choose: always(browser.Action{Op: browser.OpDone}), Actions: 5}.Run(context.Background(), "goal")
	if result.Status != StatusDone || result.Decisions != 2 {
		t.Fatalf("stopped %v after %d decisions, want done after 2", result.Status, result.Decisions)
	}
}

func TestATextErrorTypesNothing(t *testing.T) {
	tab := &fakeTab{page: browser.Page{Fingerprint: "p", Elements: []browser.Element{{Index: 4, Role: browser.RoleSearchbox, Label: "Destination"}}}}
	failing := func(context.Context, string, browser.Page, browser.Element) (string, error) {
		return "", errors.New("the text model refused")
	}
	result := Loop{Browser: tab.browser(), Choose: always(browser.Action{Op: browser.OpTypeText, Element: 4}), Write: failing, Actions: 5}.Run(context.Background(), "goal")
	assertBlocked(t, result, "the text model refused")
	if len(tab.acts) != 0 {
		t.Fatalf("typed %d times", len(tab.acts))
	}
}
