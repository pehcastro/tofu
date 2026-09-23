package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	library "tofu/internal/llm/models"
)

const (
	pressesPastEveryRow = 64
	shippedRoot         = "../../library"
)

func shippedFixture() (library.Library, error) {
	return library.Load([]library.Layer{{Name: "library", Origin: "library", FS: os.DirFS(shippedRoot)}})
}

func pickTheLastModelOffered(app *App) {
	for range pressesPastEveryRow {
		app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestPickingAModelFromAnotherSubscriptionReachesTheTurn(t *testing.T) {
	ran := make(chan string, 1)
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  bothWires,
		Models: shippedFixture,
		Turn:   func(_ context.Context, wire, _ string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) { ran <- wire },
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	if app.wire != "codex" {
		t.Fatalf("the app opened on wire %q, want the first signed wire codex", app.wire)
	}
	typeText(app, "/models")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if opened := app.View().Content; !strings.Contains(opened, "claude-sub/claude-sonnet-5") {
		t.Fatalf("/models did not open a picker naming the models\n%s", opened)
	}
	pickTheLastModelOffered(app)
	if app.wire != "anthropic" {
		t.Fatalf("the pick left the app on wire %q, want anthropic", app.wire)
	}
	typeText(app, "read one file")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case wire := <-ran:
		if wire != "anthropic" {
			t.Fatalf("the turn ran on %q, want the picked anthropic", wire)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never started")
	}
}

func TestThePickerOffersOnlyTheSubscriptionsTheTurnCanRun(t *testing.T) {
	loaded, err := shippedFixture()
	if err != nil {
		t.Fatal(err)
	}
	if _, carried := loaded.ForWire("codex"); !carried {
		t.Fatal("the library carries no codex subscription, so a wire nobody signed in to cannot be shown missing")
	}
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: anthropicAlone, Models: shippedFixture})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app.openPicker()
	signed := map[string]bool{}
	for _, wire := range app.wires {
		signed[wire.Provider] = true
	}
	if len(app.picker.Groups) != len(signed) {
		t.Fatalf("the picker shows %d subscriptions and the turn can run %d", len(app.picker.Groups), len(signed))
	}
	for _, group := range app.picker.Groups {
		if !signed[group.Source] {
			t.Errorf("the picker offers %s, which no signed wire runs", group.Source)
		}
	}
	if shown := app.View().Content; strings.Contains(shown, "codex-sub/") {
		t.Errorf("the picker names a model of a subscription nobody signed in to\n%s", shown)
	}
}

func TestThePickSurvivesThePickerClosing(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: bothWires, Models: shippedFixture})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app.openPicker()
	pickTheLastModelOffered(app)
	if app.current != viewChat {
		t.Fatalf("enter left the app on view %d, want chat", app.current)
	}
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.wire != "anthropic" {
		t.Fatalf("the pick did not survive the screen closing: the app is on wire %q", app.wire)
	}
	if !strings.Contains(app.View().Content, "claude-sub/claude-opus-5") {
		t.Errorf("the header does not name the subscription and model the next turn runs\n%s", app.View().Content)
	}
}

func TestTheShippedLibraryIsWhatThePickerReadsWhenNobodyPassesOne(t *testing.T) {
	loaded, err := New(Options{Repo: testRepo, Now: fixedClock()}).options.Models()
	if err != nil {
		t.Skipf("the model library on this machine does not load, so the default cannot be read here: %v", err)
	}
	if len(loaded.Models) == 0 {
		t.Fatal("the default model source carries no model, so /models would open on nothing")
	}
}
