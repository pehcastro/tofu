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
	snapshots int
	moveAt    int
	acts      []browser.Action
}

func (f *fakeTab) browser() Browser {
	return Browser{
		Snapshot: func(context.Context) (browser.Page, error) {
			if f.snapshots++; f.snapshots == f.moveAt {
				f.page.Elements = append(f.page.Elements, browser.Element{Index: 99, Role: browser.RoleButton, Label: "Cookie banner"})
			}
			return f.page, nil
		},
		Act: func(_ context.Context, _ browser.Page, action browser.Action) (browser.Stale, error) {
			if f.staleActs > 0 {
				f.staleActs--
				return browser.StaleCovered, nil
			}
			f.acts = append(f.acts, action)
			if f.changes {
				f.page.Fingerprint = fmt.Sprint("hotel-", len(f.acts))
			}
			return browser.StaleNone, nil
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

func inTurn(actions ...browser.Action) Chooser {
	return func(context.Context, string, browser.Page, []Step) (Choice, error) {
		action := actions[0]
		actions = actions[min(1, len(actions)-1):]
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

func TestAStaleStepIsRecordedWithItsReasonAndTheNextStateCarriesIt(t *testing.T) {
	tab := &fakeTab{changes: true, staleActs: 1}
	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	result := recordedLoop(t, tab, wire, 2).Run(context.Background(), "goal")
	assertBlocked(t, result, "action budget")
	if len(tab.acts) != 2 || len(wire.posted) != 4 || len(result.Steps) != 3 || result.Steps[0].Stale != browser.StaleCovered {
		t.Fatalf("ran %d actions over %d decisions into %+v; want 2 over 4 into 3, the first covered", len(tab.acts), len(wire.posted), result.Steps)
	}
	if !strings.Contains(string(wire.posted[1]), `"did_not_run":"covered"`) {
		t.Fatalf("the decision after a covered step was asked on %s", wire.posted[1])
	}
}

func TestACoveredTargetStopsAfterThreeDecisionsNotTheBudget(t *testing.T) {
	tab := &fakeTab{staleActs: 1000}
	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	result := recordedLoop(t, tab, wire, 30).Run(context.Background(), "goal")
	assertBlocked(t, result, "did not run 3 times in a row: covered")
	if len(tab.acts) != 0 || len(wire.posted) != 3 || result.Decisions != 3 {
		t.Fatalf("ran %d actions over %d Jev calls and %d decisions, want none over 3", len(tab.acts), len(wire.posted), result.Decisions)
	}
}

func TestTheStaleCountRestartsOnAnotherTarget(t *testing.T) {
	tab := &fakeTab{staleActs: 1000}
	tab.page = hotelPage(t)
	first, second := browser.Action{Op: browser.OpClick, Element: 8}, browser.Action{Op: browser.OpClick, Element: 9}
	result := Loop{Browser: tab.browser(), Choose: inTurn(first, first, second, second, second), Actions: 30}.Run(context.Background(), "goal")
	assertBlocked(t, result, `"View The Glasshouse" did not run 3 times in a row`)
	if result.Decisions != 5 {
		t.Fatalf("stopped after %d decisions, want 5", result.Decisions)
	}
}

func TestDoneAfterTheElementsMovedIsDecidedAgain(t *testing.T) {
	tab := &fakeTab{page: browser.Page{Fingerprint: "p"}, moveAt: 2}
	result := Loop{Browser: tab.browser(), Choose: always(browser.Action{Op: browser.OpDone}), Actions: 5}.Run(context.Background(), "goal")
	if result.Status != StatusDone || result.Decisions != 2 || len(result.Page.Elements) != 1 {
		t.Fatalf("stopped %v after %d decisions on %+v, want done after 2 on the moved page", result.Status, result.Decisions, result.Page)
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
